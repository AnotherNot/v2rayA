// SPDX-License-Identifier: GPL-2.0
//
// Probe for the app-router feature. It attaches where the router will attach
// (cgroup/connect4 and cgroup/connect6), reads the same identity the router
// will read — the executable behind the socket, through CO-RE — and counts
// what it sees. Every connect is allowed: the probe changes no traffic.
//
// The object next to this file is generated and committed, the way the core's
// .pb.go files are, so `./build-all.sh` needs no clang. Regenerate it from the
// service module root after editing this file:
//
//	clang -O2 -g -target bpf -c kernel/approuter/bpf/probe.bpf.c -o kernel/approuter/bpf/probe.bpf.o
//
// The object is little-endian BPF, which is every architecture the release
// builds for.
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// Minimal kernel types, relocated against the running kernel's BTF. The
// layouts are deliberately incomplete: only the fields read below matter, and
// preserve_access_index keeps the field access as a CO-RE relocation.
#pragma clang attribute push(__attribute__((preserve_access_index)), apply_to = record)
struct super_block { __u32 s_dev; };
struct inode { struct super_block *i_sb; unsigned long i_ino; };
struct file { struct inode *f_inode; };
struct mm_struct { struct file *exe_file; };
struct task_struct { struct mm_struct *mm; };
#pragma clang attribute pop

// The identity of an executable: its file, not its name. Two binaries called
// the same are told apart, and a replaced binary is a different executable.
struct probe_exe_key {
	__u32 dev;
	__u32 pad;
	__u64 ino;
};

struct probe_exe_stat {
	__u8 comm[16];
	__u64 flows;
};

// [0] IPv4 connects, [1] IPv6 connects.
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 2);
	__type(key, __u32);
	__type(value, __u64);
} connects SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 4096);
	__type(key, struct probe_exe_key);
	__type(value, struct probe_exe_stat);
} executables SEC(".maps");

static __always_inline void count_connect(__u32 family)
{
	__u64 *n = bpf_map_lookup_elem(&connects, &family);
	if (n)
		__sync_fetch_and_add(n, 1);
}

// The (device, inode) pair of the executable behind the calling task, the key
// the router's executable rules will be stored under.
static __always_inline int exe_identity(struct probe_exe_key *key)
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

static __always_inline int probe_connect(struct bpf_sock_addr *ctx, __u32 family)
{
	count_connect(family);

	struct probe_exe_key key = {};
	if (!exe_identity(&key))
		return 1;
	struct probe_exe_stat *stat = bpf_map_lookup_elem(&executables, &key);
	if (stat) {
		__sync_fetch_and_add(&stat->flows, 1);
		return 1;
	}
	struct probe_exe_stat fresh = {};
	bpf_get_current_comm(&fresh.comm, sizeof(fresh.comm));
	fresh.flows = 1;
	bpf_map_update_elem(&executables, &key, &fresh, BPF_NOEXIST);
	return 1;
}

SEC("cgroup/connect4")
int probe_connect4(struct bpf_sock_addr *ctx)
{
	return probe_connect(ctx, 0);
}

SEC("cgroup/connect6")
int probe_connect6(struct bpf_sock_addr *ctx)
{
	return probe_connect(ctx, 1);
}

char _license[] SEC("license") = "GPL";