# Route traffic by application

`app_router` is a transparent mode that decides per program instead of per
address. A cgroup BPF program reads the executable behind the socket, matches it
against the `process(...)` rules in the RoutingA editor, and either leaves the
connection alone, hands it to the outbound the rule names, or refuses it.

It is not TUN. No packet is captured and no route is changed: the kernel
rewrites the destination of a socket a rule selected, the service relays that
connection to a loopback inbound that stands for one outbound, and the core only
chooses the outgoing path.

## Enable

Settings → **Transparent proxy** → mode **By application**, transparent proxy on,
then write the rules and press Start. The rule editor is the ordinary RoutingA
editor; a rule that selects on `process(...)` belongs to this mode, and the rest
of the text keeps following the rule port's mode.

```
process(firefox) -> proxy
process(curl) && port(443) -> work
process(steam) && network(udp) -> proxy
process(transmission) -> block
process(ssh) && domain(private.example.com) -> direct
```

Rules match from top to bottom and the first match wins. A program no rule names
is not touched at all. A `process(...)` rule is inactive while another
transparent implementation is chosen; the log says how many such rules were
skipped.

## What a rule may select on

| Condition | Values |
| --- | --- |
| `process(...)` | one matcher: a bare value is a file name, `name:`, `path:` and `folder:` say which part of the program it means; `*` and `?` are wildcards |
| `ip(...)` | IPv4 or IPv6 literal, CIDR, or `first-last`; an IPv6 literal is quoted, as in `ip("2001:db8::/32")`. A rule that names one family does not match the other, and a v4-mapped IPv6 value is refused because the destination is an IPv4 one |
| `port(...)` | a port or `lo-hi`, up to two ranges per rule |
| `network(...)` | `tcp`, `udp`, or both; without it the rule covers both |
| `domain(...)`, `domains(...)` | a name: a bare value and `suffix:` are suffixes (`example.com` covers `a.example.com`, not `notexample.com`); also `full:`, `keyword:`/`contains:`, `regexp:` |

The action is `direct`, `block`, or the tag of an outbound with a selected
server. Conditions the engine cannot enforce — `protocol()`, `source()`,
`sourcePort()`, `user()`, `inboundTag()`, `ip(geoip:...)`, `ip(ext:...)`,
`domain(geosite:...)` — are an error, not something ignored: a rule that silently
lost a condition would match more than its author wrote. The same is true of an
unknown value prefix.

## Domains need the DNS module

A name is not in the packet. A rule that selects on one is matched against what
the DNS module answered: the module reports each name-and-address pair to the
service over a Unix socket in the configuration directory, waits for the service
to store it, and only then hands the answer to the application. The service keeps
the names an address is known by for the record's TTL, clamped to 10 seconds …
30 minutes, up to 8 names per address and 16384 addresses in total.

This works only when the DNS module sees the queries — **DNS mode hijack** —
and the settings page and the rule editor refuse a domain rule in any other
mode, as does the config build. A program that resolves names elsewhere (its own
DoH resolver, a hardcoded server) is not recognised this way, and neither is a
connection made to an address before its name was asked for or after its TTL
expired. Because the kernel cannot evaluate a name, a program with a domain rule
is decided by the service on every flow.

## What is never proxied

Whatever the rules say, these destinations are left alone:

- IPv4 loopback `127.0.0.0/8`, the unspecified range `0.0.0.0/8`, and multicast
  and above (`224.0.0.0` and higher);
- IPv6 `::1`, `ff00::/8` and `fe80::/10`;
- UDP to ports 67 and 68, so DHCP still answers and the machine keeps its lease.

The daemon's own sockets, the core's sockets and sockets in another network
namespace are left alone too. The last one is deliberate: another namespace is
another host, and its traffic is not this mode's to route.

## Unknown owner

If the program behind a socket cannot be identified — it exited before the
lookup, or the service cannot see its process namespace — the connection follows
**Unknown owner** in the settings: by default it is left alone, and it can be set
to be refused instead. Refusing keeps a blocking rule from failing open, at the
price of breaking anything the service cannot attribute. In a container with a
private PID namespace every program is unidentified, so with the block policy
such a container refuses every connection it cannot attribute.

## TCP, UDP and IPv6

The hooks are `connect4`/`connect6`, `sockops`, `getpeername4`/`getpeername6`,
`sendmsg4`/`sendmsg6` and `recvmsg4`/`recvmsg6`. TCP goes to the relay on
`127.0.0.1:52346`, which opens a SOCKS5 CONNECT to the class inbound; the
`getpeername` hooks keep reporting the destination the application asked for.

UDP is carried as a SOCKS5 UDP association. Each datagram destination of each
socket gets a flow address from `127.1.0.0/16`; the relay listens on `0.0.0.0`
and answers from that flow address, and the `recvmsg` hook turns the source back
into the real one. The socket is part of the identity, so a program that opens a
fresh socket per query still gets its own answers. A datagram socket is matched
per destination, so one socket talking to several places gets the right rule for
every datagram.

IPv4-mapped IPv6 addresses are treated as IPv4 throughout: the destination is
normalised before matching and `getpeername` reports the IPv4 form. Native IPv6
TCP is carried. A native IPv6 datagram is refused with `EPERM` instead of being
sent around its rule, because the kernel will not carry a datagram rewritten to
the IPv4 relay; a program is learned from a connection the service can see, so a
program that sends only native IPv6 datagrams is refused until it makes such a
connection. This is the one place where the mode fails closed on its own.

## Requirements

- Linux with cgroup v2 mounted at `/sys/fs/cgroup`, kernel 5.14 or later (the
  service refuses to attach when `SO_NETNS_COOKIE` is unsupported).
- root, or `CAP_BPF` and `CAP_NET_ADMIN` to load and attach the programs.
- The service must see the host's process namespace: the program is identified
  through `/proc`.
- Not **Lite** mode: like tproxy and redirect, the mode is not offered there.

The in-app documentation notes the same preconditions on the settings page.

### Containers

The shipped Docker profiles cannot run this mode. Attaching a cgroup program
requires the host's cgroup v2 hierarchy writable inside the container, and
identifying the program requires the host's PID namespace; the transparent
profile deliberately grants neither, because with them the container would be
routing the host's traffic with rights equivalent to `--privileged`. A container
with a private PID namespace also leaves every program unidentified, which the
mode reports in its counters rather than hiding.

What it would take, and what the profiles do not do: mount `/sys/fs/cgroup`
read-write, `--pid=host`, `--cgroupns=host`, and `CAP_BPF`/`CAP_NET_ADMIN` (or
privileged). Use a native package or the systemd unit for this mode.

## Size limits

These are bounds of the generated state, not of the interface:

| Limit | Value | When exceeded |
| --- | --- | --- |
| rules in one configuration | 256 | the engine refuses to attach and the mode does not start; the message names the count |
| rules per executable | 16 | that executable is decided by the service on every flow |
| addresses per rule | 2 IPv4 and 2 IPv6 | same |
| port ranges per rule | 2 | same |
| outbounds named by rules | 32 | the config build refuses the rule |

An executable whose rules do not fit is cached with the reserved "decide" slot
and goes through the service every time; the log names it. A rule with a domain
always takes that path.

## Watching it work

The engine writes its counters to the log every 30 seconds:

```
app router: 120 connects, 96 proxied, 20 direct, 4 blocked, 0 decided late, 96 relayed, 20 dialled directly, 0 refused
app router: 34 UDP datagrams, 30 redirected, 30 relayed, 4 direct, 0 refused, 30 answers restored
app router: 2 IPv6 connects, 0 native IPv6 datagrams refused as unredirectable
```

`decided late` is flows the service had to judge because the executable's rules
were not cached yet; `relayed` and `dialled directly` are what the relay did with
them. A mode that is on but routes nothing looks exactly like one that is off
from the outside, and these three lines are what tells them apart.

The same counters are served by `GET /api/appRouter` and shown on the settings
page as **Engine status**: the mode, whether the engine is attached, the plan's
rule and class count, and the counters above. The page reads them when it opens,
after a save, and when the refresh button is pressed; they are not polled.

## Limits and interactions

- This is a routing policy, not an isolation boundary. `block` refuses new
  connections of a named program; it does not stop a program that opens raw
  sockets or that another rule sends through `direct`.
- The first connection or datagram of a program is judged by the service and
  then cached in the kernel per executable; until then it takes the slower path.
  A program that replaces its executable file gets a new identity and is judged
  again.
- `app_router` is mutually exclusive with tproxy, redirect, TUN and system-proxy:
  choosing it removes their rules. No REDIRECT rule is installed for TCP; the
  DNS redirect is the only firewall rule, and only in hijack mode.
- DNS conditions depend on the DNS module, as described above; a DoH resolver
  the module does not answer for is invisible to them.
- No performance benchmark has been done. The per-flow cost on the slow path is
  a relay connection and a SOCKS5 handshake; the fast path is a map lookup in
  the hook.
- Starting the mode with a rule that names no `process(...)` attaches nothing and
  logs a warning: a program that routes nothing would hide the mistake behind
  normal traffic.

## Tests

Unprivileged tests cover rule translation, the decision order, the domain
matcher and the name table, and the generated config. They do not load BPF
programs:

```sh
cd service
go test -race ./kernel/approuter
go test ./kernel/v2ray -run AppRouter
```

The privileged test is opt-in and attaches only to a disposable child cgroup,
never to the host's root cgroup. It starts a local destination and a class
inbound on loopback, then runs a child process in that cgroup and checks what
the kernel did with it: a proxied flow reaches the class inbound twice (the
second flow comes from the kernel's cache), a direct flow reaches the
destination and not the inbound, a blocked flow is refused, a datagram is
carried through its own SOCKS5 association, and an executable whose rules do
not fit the kernel keeps being decided by the service on every flow — the
defect the live stand found is covered by testing every path twice.

```sh
cd service
go test -c -o /tmp/v2raya-approuter-tests ./kernel/approuter
sudo env V2RAYA_BPF_TEST=1 /tmp/v2raya-approuter-tests -test.v
```

It passed three consecutive runs on 2026-11-21, on Linux 7.2.7, in a container
sharing the host's cgroup and PID namespaces.

The live validation below is the deeper pass: real binaries, a real daemon, a
container sharing the host's cgroup namespace, and scripts kept outside this
repository.

## Observed live validation (2026-11-18, 2026-11-19)

Run on Linux 7.2.7 (amd64), in a privileged container sharing the host's cgroup
and PID namespaces, against a build of this checkout. Every phase used the same
real daemon and the core it starts; the outbound was a local SOCKS5 server that
logged each destination, the destinations were local HTTP/UDP servers, and the
"proxied" claim always means a `CONNECT` line appeared in that server's log.
Each phase was run three times; the counts below are per run.

| Phase | Checks | What it established |
| --- | --- | --- |
| 2 — rules, classes, safety net | 26 | a proxied program reaches the destination through the outbound and `getpeername` still reports what it asked for; `direct` stays off the outbound; `block` reaches neither; an unnamed program is untouched; the kernel caches an executable's decision and refuses a blocked one outright from the second connection; stopping the mode leaves the cgroup tree clean and traffic working |
| 3 — many executables, other namespaces | 17 (+1 skipped) | 300 distinct executables all reach the server; 254 fit the kernel's slots and the rest are decided by the service, which says so; a program in another network namespace is not routed; the DNS port is redirected when hijack is on, and names resolve while the engine is attached. The skipped check is the port-53 redirect: that container had no `nat` table for the chosen firewall backend |
| 4 — UDP | 25 | two destinations of one socket both leave through the outbound; answers come from the real destination, not the flow address; two sockets get their own answers and their own associations; a connected datagram socket works; a mixed program is decided by its `udp` half; `direct` datagrams stay off the outbound; a blocked datagram is dropped once and refused by the kernel afterwards; DHCP to port 67 goes direct; TCP still works |
| 5 — IPv6 | 24 | IPv6 TCP through the outbound, with `getpeername` unchanged; CIDR, range and single-address rules each match only what they name; a v4-mapped destination is reported and routed as IPv4; an IPv6 UDP datagram through the outbound; a native IPv6 UDP datagram is refused with `EPERM` instead of leaking; IPv4 still works |
| 6 — domains | 19 | the report socket exists and the module reports to it; the same program to the same address is direct before the name is resolved and proxied after it; a connection made without asking DNS again still knows the name; a CNAME chain is followed; an address no name points at stays direct; the executable with a domain rule is decided by the service. Run again twice on the binary produced by `./build-all.sh` |
| 2–5 regression | 92 (+1 skipped) | every earlier phase re-run on the phase-6 binary, no failures |

Total: 111 checks per full pass (one skipped, for the container's missing `nat`
table), three passes.

### What this found

The live runs found one defect that the unit tests could not: the reserved
"decide every flow in user space" slot was written for an executable, but the
marker that tells the kernel what the slot means was never written. The first
flow of such a program went through the service and was correct; every later
flow was let through **directly and silently**. The marker is now written once
when the maps are filled, and the stand checks a second flow as well as the
first.

### What was not observed

- The GUI in this mode: enabling it, editing the rules, the Unknown owner control.
- A core restart or daemon crash with the mode attached, and reattachment.
- 32 classes, and the 256-rule and 16-rules-per-executable boundaries in the
  direction of a refusal (the 254-slot ceiling of phase 3 is the map size, not
  the rule limit).
- The iptables DNS redirect itself (the container had no `nat` table); the
  domain checks spoke to the module's `127.2.0.17:53` listener directly.
- Port sharing: the class inbounds stay on loopback, which the code says and the
  stand confirms by using them, but no external client was pointed at them.
- Any real application. Every "program" was a small Go binary built for the
  stand, and every destination was on the same host or on loopback.
- Performance, packet loss, and behaviour under many concurrent connections.
