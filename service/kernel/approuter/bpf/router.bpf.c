// SPDX-License-Identifier: GPL-2.0
//
// Per-application routing, first stage: TCP over IPv4, one destination class.
// The connect() of a selected executable is rewritten to the local relay, which
// hands the flow to a local inbound of the core; every other connect is left
// alone.
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

#define AF_INET 2
#define IPPROTO_TCP 6

#define MAX_APPS 64

struct router_cfg {
	__u32 enabled;
	__u32 relay_port; // host order
	__u32 self_tgid;
	__u32 pad;
	__u64 netns; // netns cookie of the relay; 0 = every namespace
};

// An executable name as bpf_get_current_comm reports it: sixteen bytes, which
// two words hold without the padding encoding/binary would have to be told
// about.
struct router_comm_key {
	__u64 w0;
	__u64 w1;
};

// The destination the application asked for, kept while the socket talks to the
// relay instead.
struct router_orig {
	__u32 ip4;  // network order
	__u16 port; // host order
	__u16 pad;
	__u32 pid;
	__u32 pad2;
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
// The executables whose connects are routed. Matched on the name the kernel
// reports for the task, which is what an application list names.
MAP(app_names, BPF_MAP_TYPE_HASH, MAX_APPS, struct router_comm_key, __u8);
// The original destination, keyed by socket, so getpeername can report it.
MAP(by_cookie, BPF_MAP_TYPE_LRU_HASH, 8192, __u64, struct router_orig);
// ... and keyed by the client's own address, which is the handle the relay has
// when it accepts the connection.
MAP(by_port, BPF_MAP_TYPE_LRU_HASH, 8192, struct router_port_key, struct router_orig);

// Diagnostics. The order is mirrored by the loader.
MAP(stats, BPF_MAP_TYPE_ARRAY, 8, __u32, __u64);

enum {
	STAT_CONNECT = 0,         // every TCP connect the hook saw
	STAT_IMPLICIT_DIRECT = 1, // the destination is never proxied
	STAT_NOT_SELECTED = 2,    // the executable is not in the list
	STAT_SELF = 3,            // the daemon's own socket
	STAT_FOREIGN_NETNS = 4,   // another network namespace: containers, and the like
	STAT_REDIRECTED = 5,      // the destination was rewritten to the relay
	STAT_PORT_WRITTEN = 6,    // ... and reached the relay's lookup table
	STAT_GETPEERNAME = 7,     // the original destination was reported back
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

// Never proxied whatever the list says: loopback, the unspecified range and
// multicast. A rule that wants loopback joins later; the relay's own port must
// never come back to the relay.
static __always_inline int implicit_direct4(__u32 ip4)
{
	if ((ip4 >> 24) == 127)
		return 1;
	if ((ip4 >> 24) == 0)
		return 1;
	return ip4 >= 0xe0000000; // multicast and above
}

static __always_inline int selected(void)
{
	__u8 comm[16] = {};
	bpf_get_current_comm(&comm, sizeof(comm));
	struct router_comm_key key = {};
	__builtin_memcpy(&key, comm, sizeof(comm));
	return bpf_map_lookup_elem(&app_names, &key) != 0;
}

SEC("cgroup/connect4")
int router_connect4(struct bpf_sock_addr *ctx)
{
	// Only TCP and only IPv4: UDP and IPv6 are later stages, and a hook that
	// half-serves them would leave the list's traffic on the direct path
	// without saying so.
	if (ctx->protocol != IPPROTO_TCP)
		return 1;
	count(STAT_CONNECT);

	struct router_cfg *c = cfgp();
	if (!c || !c->enabled)
		return 1;
	if (foreign_netns(ctx)) {
		count(STAT_FOREIGN_NETNS);
		return 1;
	}
	if ((bpf_get_current_pid_tgid() >> 32) == c->self_tgid) {
		count(STAT_SELF);
		return 1;
	}

	__u32 ip4 = bpf_ntohl(ctx->user_ip4);
	__u16 port = bpf_ntohs(ctx->user_port);
	if (implicit_direct4(ip4) || port == (__u16)c->relay_port) {
		count(STAT_IMPLICIT_DIRECT);
		return 1;
	}
	if (!selected()) {
		count(STAT_NOT_SELECTED);
		return 1;
	}

	struct router_orig o = {};
	o.ip4 = ctx->user_ip4;
	o.port = port;
	o.pid = bpf_get_current_pid_tgid() >> 32;
	__u64 cookie = bpf_get_socket_cookie(ctx);
	bpf_map_update_elem(&by_cookie, &cookie, &o, BPF_ANY);

	ctx->user_ip4 = bpf_htonl(0x7f000001);
	ctx->user_port = bpf_htons(c->relay_port);
	count(STAT_REDIRECTED);
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
	if (ctx->protocol != IPPROTO_TCP)
		return 1;
	__u64 cookie = bpf_get_socket_cookie(ctx);
	struct router_orig *o = bpf_map_lookup_elem(&by_cookie, &cookie);
	if (!o)
		return 1;
	ctx->user_ip4 = o->ip4;
	ctx->user_port = bpf_htons(o->port);
	count(STAT_GETPEERNAME);
	return 1;
}

char _license[] SEC("license") = "GPL";