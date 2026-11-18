// SPDX-License-Identifier: GPL-2.0
//
// Per-application routing: the connect() of a selected executable is matched
// against that executable's ordered rule list and is sent to a local relay,
// left alone, or refused. UDP is handled one datagram at a time: each
// destination of a socket gets a loopback "flow address" that stands for it,
// so one socket can talk to many destinations and the relay can answer from
// the address the application asked for. Sockets in one cgroup subtree and one
// network namespace only.
//
// A rule carries a set of destination addresses, ports and an action; the first
// rule of the executable's list that matches decides. Rules live in arrays so
// the whole walk is bounded and the verifier can see its extent.
//
// Two things are called "class" elsewhere and are kept apart here:
//   - the ordered rule list of one executable (exec_rules, keyed by exe2rules);
//   - the local inbound a proxy rule hands the flow to (the rule's inbound_id).
//
// The object is generated and committed, the way the core's .pb.go files are,
// so ./build-all.sh needs no clang. Regenerate it from the service module root
// after editing this file:
//
//	clang -O2 -g -target bpf -c kernel/approuter/bpf/router.bpf.c -o kernel/approuter/bpf/router.bpf.o
//
// The object is little-endian BPF, which is every architecture the release
// builds for.
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_core_read.h>

// Minimal kernel types, relocated against the running kernel's BTF (CO-RE).
// Only the fields read below matter.
#pragma clang attribute push(__attribute__((preserve_access_index)), apply_to = record)
struct super_block { __u32 s_dev; };
struct inode { struct super_block *i_sb; unsigned long i_ino; };
struct file { struct inode *f_inode; };
struct mm_struct { struct file *exe_file; };
struct task_struct { struct mm_struct *mm; };
#pragma clang attribute pop

#define AF_INET 2
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// The transport bits a rule carries. Zero on a rule means "either".
#define NET_TCP 1
#define NET_UDP 2

#define ACT_DIRECT 0
#define ACT_PROXY 1
#define ACT_BLOCK 2
#define ACT_DECIDE 3 // unknown executable: the relay decides once and caches it

#define MAX_RULES 256
#define MAX_EXE_RULES 256
#define RULES_PER_EXE 16
#define MAX_V4 2
#define MAX_PORTS 2

// An executable whose matching rules do not fit in its list: every flow of it
// is decided in user space instead of being walked here. Failing instead would
// silently stop applying its rules.
#define RULES_DECIDE 0xffffffffu

struct router_cfg {
	__u32 enabled;
	__u32 relay_port; // host order
	__u32 self_tgid;
	__u32 core_tgid; // the core's own sockets are never routed either
	__u64 netns;     // netns cookie of the relay; 0 = every namespace
};

// The identity of an executable: its file, not its name. Two binaries called
// the same are told apart, and a replaced binary is a different executable.
struct router_exe_key {
	__u32 dev;
	__u32 pad;
	__u64 ino;
};

// One rule, reduced to what the kernel can evaluate. Addresses are host order,
// ports host order, ranges inclusive.
struct router_rule {
	__u8 action;        // ACT_*
	__u8 any_host;
	__u8 any_port;
	__u8 n_v4;
	__u8 n_port;
	__u8 inbound_id;    // for ACT_PROXY: which local inbound to hand the flow to
	__u8 network;       // NET_*; zero = both
	__u8 pad[1];
	struct { __u32 lo; __u32 hi; } v4[MAX_V4];
	struct { __u16 lo; __u16 hi; } port[MAX_PORTS];
};

// The ordered rules of one executable.
struct router_exec_rules {
	__u32 n;
	__u32 rule[RULES_PER_EXE];
};

// The destination the application asked for, kept while the socket talks to the
// relay instead. addr holds an IPv4 address in its last four bytes.
struct router_orig {
	__u8 addr[16];
	__u16 port; // host order
	__u16 pad0;
	__u32 pid; // tgid
	__u32 exe_dev;
	__u32 pad1;
	__u64 exe_ino;
	__u64 cookie; // the socket the flow belongs to
	__u8 action;
	__u8 family;
	__u8 proto;
	__u8 inbound_id;
	__u8 pad3[4];
};

// What the relay sees as its peer: the client's local address and port, both
// host order. IPv4 only.
struct router_port_key {
	__u32 ip;
	__u32 port;
};

#define MAP(name, kind, entries, key_t, val_t) \
	struct { __uint(type, kind); __uint(max_entries, entries); __type(key, key_t); __type(value, val_t); } name SEC(".maps")

MAP(cfg, BPF_MAP_TYPE_ARRAY, 1, __u32, struct router_cfg);
MAP(brules, BPF_MAP_TYPE_ARRAY, MAX_RULES, __u32, struct router_rule);
MAP(exec_rules, BPF_MAP_TYPE_ARRAY, MAX_EXE_RULES, __u32, struct router_exec_rules);
// Executable -> its rule list. An entry the kernel was not given sends the
// flow to the relay, which resolves, decides and caches it.
MAP(exe2rules, BPF_MAP_TYPE_HASH, 4096, struct router_exe_key, __u32);
// Executables that are never redirected: the daemon and the core.
MAP(exempt_exe, BPF_MAP_TYPE_HASH, 256, struct router_exe_key, __u8);
// The original destination, so getpeername can report it, and so the relay can
// recover it from the peer address it sees.
MAP(by_cookie, BPF_MAP_TYPE_LRU_HASH, 8192, __u64, struct router_orig);
MAP(by_port, BPF_MAP_TYPE_LRU_HASH, 8192, struct router_port_key, struct router_orig);
// One entry per UDP flow, keyed by the loopback flow address the destination
// was rewritten to. The address stands for one (process, destination) pair;
// the recvmsg hook reads it to give the application the real source back.
MAP(flows, BPF_MAP_TYPE_LRU_HASH, 4096, __u32, struct router_orig);

MAP(stats, BPF_MAP_TYPE_ARRAY, 32, __u32, __u64);

enum {
	STAT_CONNECT = 0,         // every TCP connect the hook saw
	STAT_IMPLICIT_DIRECT = 1, // the destination is never proxied
	STAT_SELF = 2,            // the daemon's own socket
	STAT_FOREIGN_NETNS = 3,   // another network namespace
	STAT_DECIDE = 4,          // the executable was unknown: the relay decides
	STAT_NO_RULE = 5,         // known executable, no rule matched
	STAT_RULE_DIRECT = 6,
	STAT_RULE_PROXY = 7,
	STAT_RULE_BLOCK = 8,
	STAT_REDIRECTED = 9,
	STAT_PORT_WRITTEN = 10,
	STAT_GETPEERNAME = 11,
	STAT_EXEMPT = 12,
	STAT_RULES_FULL = 13, // the executable has more rules than fit
	STAT_SEND = 14,         // UDP datagrams the send hooks saw
	STAT_UDP_REDIRECTED = 15,
	STAT_UDP_NOFLOW = 16,   // no loopback flow address was free
	STAT_UDP_REWRITTEN = 17, // answers turned back into the real source
	STAT_UDP_RECVMSG = 18,   // every UDP answer the hook looked at
};

static __always_inline void count(__u32 i)
{
	__u64 *v = bpf_map_lookup_elem(&stats, &i);
	if (v)
		__sync_fetch_and_add(v, 1);
}

static __always_inline struct router_cfg *cfgp(void)
{
	__u32 zero = 0;
	return bpf_map_lookup_elem(&cfg, &zero);
}

// A socket outside the relay's network namespace is left alone: a container's
// cgroup sits under the root one, and its traffic is not this host's.
static __always_inline int foreign_netns(void *ctx)
{
	struct router_cfg *c = cfgp();
	return c && c->netns && bpf_get_netns_cookie(ctx) != c->netns;
}

// The (device, inode) pair of the executable behind the calling task.
static __always_inline int exe_identity(struct router_exe_key *key)
{
	struct task_struct *task = bpf_get_current_task_btf();
	struct mm_struct *mm = BPF_CORE_READ(task, mm);
	if (!mm)
		return 0;
	struct file *exe = BPF_CORE_READ(mm, exe_file);
	if (!exe)
		return 0;
	struct inode *in = BPF_CORE_READ(exe, f_inode);
	if (!in)
		return 0;
	key->dev = BPF_CORE_READ(in, i_sb, s_dev);
	key->ino = BPF_CORE_READ(in, i_ino);
	return key->ino != 0;
}

// Addresses that are never proxied whatever the rules say: loopback, the
// unspecified range and multicast.
static __always_inline int implicit_direct4(__u32 ip4)
{
	if ((ip4 >> 24) == 127)
		return 1;
	if ((ip4 >> 24) == 0)
		return 1;
	return ip4 >= 0xe0000000; // multicast and above
}

// A datagram to the DHCP ports is answered by the local network: proxying it
// would leave the machine without a lease.
static __always_inline int implicit_direct_udp(__u32 ip4, __u16 port)
{
	if (implicit_direct4(ip4))
		return 1;
	return port == 67 || port == 68;
}

// A loopback flow address stands for one UDP destination of one socket: the
// relay replies from it and the recvmsg hook turns it back into the real
// source. The socket is part of the identity, not just the process: a program
// that opens a fresh socket per datagram (a resolver does) would otherwise
// share an address with its previous socket and receive that socket's answers.
// 127.1.0.0/16 is used, so a datagram from a real local service cannot be
// mistaken for a flow. 0 means no address was free; the caller refuses the send
// rather than borrowing another flow's address.
static __always_inline __u32 udp_flow_addr(struct router_orig *o, __u64 cookie)
{
	o->cookie = cookie;
	const __u32 *w = (const __u32 *)o->addr;
	__u32 h = w[0] * 2654435761u ^ w[1] * 40503u ^ w[2] * 2246822519u ^ w[3] * 3266489917u;
	h ^= ((__u32)o->port << 16) | o->pid;
	h ^= (__u32)cookie * 2654435761u ^ (__u32)(cookie >> 32);
	h ^= (__u32)o->action * 668265263u;
	h ^= h >> 15;
	h *= 2246822519u;
	h ^= h >> 13;
#pragma unroll
	for (int i = 0; i < 8; i++) {
		__u32 addr = 0x7f010000 | ((h + i) & 0x0000ffff);
		struct router_orig *ex = bpf_map_lookup_elem(&flows, &addr);
		if (!ex) {
			bpf_map_update_elem(&flows, &addr, o, BPF_NOEXIST);
			return addr;
		}
		if (ex->cookie == cookie && !__builtin_memcmp(ex->addr, o->addr, 16) &&
		    ex->port == o->port && ex->action == o->action && ex->pid == o->pid)
			return addr;
	}
	return 0;
}

static __always_inline int rule_matches(const struct router_rule *r, __u32 ip4, __u16 port, __u8 pbit)
{
	if (r->network && !(r->network & pbit))
		return 0;
	if (!r->any_port) {
		int ok = 0;
#pragma unroll
		for (int i = 0; i < MAX_PORTS; i++) {
			if (i >= r->n_port)
				break;
			if (port >= r->port[i].lo && port <= r->port[i].hi) {
				ok = 1;
				break;
			}
		}
		if (!ok)
			return 0;
	}
	if (!r->any_host) {
		int ok = 0;
#pragma unroll
		for (int i = 0; i < MAX_V4; i++) {
			if (i >= r->n_v4)
				break;
			if (ip4 >= r->v4[i].lo && ip4 <= r->v4[i].hi) {
				ok = 1;
				break;
			}
		}
		if (!ok)
			return 0;
	}
	return 1;
}

// The first matching rule of the executable's list decides. inbound_out is set
// only for ACT_PROXY.
static __always_inline int decide_exec(__u8 *inbound_out, __u32 slot, __u32 ip4, __u16 port, __u8 pbit)
{
	*inbound_out = 0;
	struct router_exec_rules *er = bpf_map_lookup_elem(&exec_rules, &slot);
	if (!er)
		return ACT_DIRECT;
	if (er->n == RULES_DECIDE) {
		count(STAT_RULES_FULL);
		return ACT_DECIDE;
	}
#pragma unroll
	for (int i = 0; i < RULES_PER_EXE; i++) {
		if (i >= er->n)
			break;
		__u32 ri = er->rule[i];
		struct router_rule *r = bpf_map_lookup_elem(&brules, &ri);
		if (!r)
			break;
		if (!rule_matches(r, ip4, port, pbit))
			continue;
		*inbound_out = r->inbound_id;
		switch (r->action) {
		case ACT_PROXY:
			count(STAT_RULE_PROXY);
			break;
		case ACT_BLOCK:
			count(STAT_RULE_BLOCK);
			break;
		default:
			count(STAT_RULE_DIRECT);
			break;
		}
		return r->action;
	}
	count(STAT_NO_RULE);
	return ACT_DIRECT;
}

// record hands the original destination to the relay keyed by the socket and
// counts the redirection. The caller has already rewritten the destination.
static __always_inline int record(struct bpf_sock_addr *ctx, struct router_orig *o)
{
	__u64 cookie = bpf_get_socket_cookie(ctx);
	bpf_map_update_elem(&by_cookie, &cookie, o, BPF_ANY);
	count(STAT_REDIRECTED);
	return 1;
}

// Shared by the connect and sendmsg hooks: both carry the destination the
// application asked for in the same place. connected is true for a connect(),
// where the socket itself is rewritten and getpeername must keep answering
// with the destination; a datagram socket is rewritten per datagram instead.
static __always_inline int v4_hook(struct bpf_sock_addr *ctx, __u8 pbit, int connected)
{
	struct router_cfg *c = cfgp();
	if (!c || !c->enabled)
		return 1;
	if (foreign_netns(ctx)) {
		count(STAT_FOREIGN_NETNS);
		return 1;
	}
	__u32 tgid = bpf_get_current_pid_tgid() >> 32;
	if (tgid == c->self_tgid || (c->core_tgid && tgid == c->core_tgid)) {
		count(STAT_SELF);
		return 1;
	}

	__u32 ip4 = bpf_ntohl(ctx->user_ip4);
	__u16 port = bpf_ntohs(ctx->user_port);
	int implicit = pbit == NET_TCP ? implicit_direct4(ip4) : implicit_direct_udp(ip4, port);
	if (implicit || port == (__u16)c->relay_port) {
		count(STAT_IMPLICIT_DIRECT);
		return 1;
	}

	struct router_orig o = {};
	o.family = AF_INET;
	o.proto = pbit == NET_TCP ? IPPROTO_TCP : IPPROTO_UDP;
	o.port = port;
	o.pid = tgid;
	__builtin_memcpy(o.addr + 12, &ctx->user_ip4, 4);

	struct router_exe_key key = {};
	if (!exe_identity(&key)) {
		// Nothing to match on: let the relay look the process up.
		count(STAT_DECIDE);
		o.action = ACT_DECIDE;
	} else {
		o.exe_dev = key.dev;
		o.exe_ino = key.ino;
		if (bpf_map_lookup_elem(&exempt_exe, &key)) {
			count(STAT_EXEMPT);
			return 1;
		}
		__u32 *slot = bpf_map_lookup_elem(&exe2rules, &key);
		if (!slot) {
			count(STAT_DECIDE);
			o.action = ACT_DECIDE;
		} else {
			__u8 inbound = 0;
			int act = decide_exec(&inbound, *slot, ip4, port, pbit);
			switch (act) {
			case ACT_BLOCK:
				return 0; // EPERM: the datagram or connection is refused
			case ACT_PROXY:
				o.action = ACT_PROXY;
				o.inbound_id = inbound;
				break;
			case ACT_DECIDE:
				o.action = ACT_DECIDE;
				break;
			default:
				return 1; // direct: the socket is untouched
			}
		}
	}

	if (pbit == NET_UDP) {
		__u64 cookie = bpf_get_socket_cookie(ctx);
		__u32 flow = udp_flow_addr(&o, cookie);
		if (!flow) {
			count(STAT_UDP_NOFLOW);
			return 0;
		}
		ctx->user_ip4 = bpf_htonl(flow);
		ctx->user_port = bpf_htons(c->relay_port);
		count(STAT_UDP_REDIRECTED);
		if (connected) {
			// A connected datagram socket has a peer to report, and the
			// application may ask for it before the first datagram.
			bpf_map_update_elem(&by_cookie, &cookie, &o, BPF_ANY);
		}
		return 1;
	}

	ctx->user_ip4 = bpf_htonl(0x7f000001);
	ctx->user_port = bpf_htons(c->relay_port);
	return record(ctx, &o);
}

SEC("cgroup/connect4")
int router_connect4(struct bpf_sock_addr *ctx)
{
	// A connect() on a datagram socket is the connected-UDP case: the socket
	// gets one flow address, and its datagrams then name that address.
	if (ctx->protocol == IPPROTO_TCP) {
		count(STAT_CONNECT);
		return v4_hook(ctx, NET_TCP, 0);
	}
	if (ctx->protocol == IPPROTO_UDP) {
		count(STAT_SEND);
		return v4_hook(ctx, NET_UDP, 1);
	}
	return 1;
}

// One datagram, one decision: a UDP socket can talk to many destinations, and
// each of them gets its own flow address.
SEC("cgroup/sendmsg4")
int router_sendmsg4(struct bpf_sock_addr *ctx)
{
	if (ctx->protocol != IPPROTO_UDP)
		return 1;
	count(STAT_SEND);
	return v4_hook(ctx, NET_UDP, 0);
}

// A datagram that came from a flow address is really from the destination the
// application asked for: the application must see that destination, or it will
// drop the answer as coming from a stranger.
SEC("cgroup/recvmsg4")
int router_recvmsg4(struct bpf_sock_addr *ctx)
{
	if (ctx->protocol != IPPROTO_UDP)
		return 1;
	count(STAT_UDP_RECVMSG);
	__u32 key = bpf_ntohl(ctx->user_ip4);
	struct router_orig *o = bpf_map_lookup_elem(&flows, &key);
	if (!o || o->family != AF_INET)
		return 1;
	__builtin_memcpy(&ctx->user_ip4, o->addr + 12, 4);
	ctx->user_port = bpf_htons(o->port);
	count(STAT_UDP_REWRITTEN);
	return 1;
}

// The relay has to learn the destination by something it can see, and what it
// sees is its peer: the client's source address and port. TCP_CONNECT_CB runs
// once the port is bound and before the handshake, so the relay can never win
// the race and accept first.
SEC("sockops")
int router_sockops(struct bpf_sock_ops *s)
{
	if (s->op != BPF_SOCK_OPS_TCP_CONNECT_CB)
		return 1;

	__u64 cookie = bpf_get_socket_cookie(s);
	struct router_orig *o = bpf_map_lookup_elem(&by_cookie, &cookie);
	if (!o)
		return 1;

	struct router_port_key key = {};
	if (s->family == AF_INET) {
		// local_ip4 comes from skc_rcv_saddr, network order; local_port from
		// skc_num, which the kernel keeps in host order.
		key.ip = bpf_ntohl(s->local_ip4);
	} else {
		// A redirected IPv6 socket holds the v4-mapped relay address, which
		// is a constant here; reading local_ip6[3] would make clang compute
		// the offset in a register, and the verifier refuses to dereference
		// a ctx pointer carrying a computed offset.
		key.ip = 0x7f000001;
	}
	key.port = s->local_port;
	bpf_map_update_elem(&by_port, &key, o, BPF_ANY);
	count(STAT_PORT_WRITTEN);
	return 1;
}

// A redirected socket must keep reporting the destination the application
// asked for, or an application that checks it misbehaves.
SEC("cgroup/getpeername4")
int router_getpeername4(struct bpf_sock_addr *ctx)
{
	if (ctx->protocol != IPPROTO_TCP && ctx->protocol != IPPROTO_UDP)
		return 1;
	__u64 cookie = bpf_get_socket_cookie(ctx);
	struct router_orig *o = bpf_map_lookup_elem(&by_cookie, &cookie);
	if (!o || o->family != AF_INET)
		return 1;
	__builtin_memcpy(&ctx->user_ip4, o->addr + 12, 4);
	ctx->user_port = bpf_htons(o->port);
	count(STAT_GETPEERNAME);
	return 1;
}

char _license[] SEC("license") = "GPL";