# v2rayA user guide

How to install v2rayA, connect your first node, and run it day to day. No
command line is required except for installing; everything else happens in
the browser interface.

The developer documents live next to this file and are linked where they
take over: [running-from-source.md](running-from-source.md) builds this
checkout and proves it works, and
[testing-on-a-real-machine.md](testing-on-a-real-machine.md) verifies one
feature at a time on a machine that already runs v2rayA.

## Install

v2rayA is two binaries that must match versions: `v2raya` (the service and
the web interface) and `v2raya_core` (the proxy core). The packages below
ship both together with a service definition; the exact commands per
distribution are in the repository [README.md](../README.md#install).

After enabling the service, open **http://127.0.0.1:2017** in a browser. If
the browser runs on another machine, the address is the server's address with
port 2017 — see the warning about registering first in [Quick start](#first-run).

A Docker image is published as well (`install/docker/` in the checkout
describes it). The data directory holds every setting; back it up before an
upgrade (see [Back up](#back-up)).

## First run

### 1. Register

The first account you register becomes the administrator; there is no other
account. On a shared network, restrict who can reach port 2017 **before**
you register, or bind the service to loopback only. A forgotten password is
reset with `v2raya --reset-password` while the service is stopped.

### 2. Import a node

On the **Proxies** page, **Import** takes share links (`vless://`,
`vmess://`, `ss://`, `trojan://` and the rest, one per line), a QR code
image, or a subscription address. Subscriptions live on their own page and
can refresh by hand or on a schedule.

Two link details worth knowing up front:

- A VLESS link may carry an `encryption` value for the new encryption
  format. v2rayA keeps it from import to configuration; a link that loses it
  builds a tunnel the server cannot read, so if you edit a node, save it
  through the editor rather than retyping the link.
- An XHTTP (`xhttp`) link may carry `mode`, `x_padding_bytes` and an `extra`
  object in the shape other clients write. All three are understood and end
  up in the same settings the editor writes.

### 3. Connect and check

Press **Start** on the dashboard. Applications reach the core through the
local ports:

| Port | What it is |
| --- | --- |
| `127.0.0.1:20170` | SOCKS5 **with your routing rules** (the only inbound on by default) |

Point a browser at the 20170 port (or set the system proxy to it) and open a
site that shows your address. Then open the same site directly: the two
addresses must differ. If they are equal, the node carries no traffic —
check that the core is running and the node is selected before touching any
rule.

Why 20170 sees your rules — every inbound carries a tag, and a rule
fires only when its tags include that inbound:

```
                        inbound tag         what matches it
                        ─────────         ───────────────
client ──► 20170 SOCKS ──► rule-socks ──► YOUR RULES ──► direct / proxy / block / group
    │                                     node hosts ──► direct (loop protection)
    │
    └──► (optional plain SOCKS/HTTP/VMess inbounds ──► default route ──► proxy)
```

So extra inbounds you enable under **Settings → Address and Ports** mean
"everything through the proxy" (except the nodes’ own hosts, which go
direct so the tunnel never tries to carry itself). If the default outbound
is a balancing group, the default route balances between its members —
user rules still do not apply there.

## Everyday management

### Nodes and subscriptions

Nodes are edited from their menu (**Modify**); saving keeps every parameter
the link carried. Nodes are removed from the same menu. A subscription is
updated from the **Subscriptions** page; the mode next to the schedule
decides whether the update itself goes through the proxy.

### Groups

Nodes are used through proxy groups. `proxy` always exists; more are created
from the group button in the top bar. A node joins a group from its menu, or
select several nodes and use **Add to proxy group**. A group with several
connected members uses the one with the lowest measured latency, or spreads
connections when its type is Random; the node menu on the dashboard pins
one member so the group uses it alone. A pinned member has no failover,
a balanced group routes around dead members. The group
settings set the probe address and interval.

### Routing modes

**Settings → Traffic Splitting Mode of Rule Port** decides what the rule
inbounds and the transparent proxy do with traffic: everything through
the proxy, proxy except the allowlist, proxy only the blocklist, or your own
RoutingA rules. RoutingA is documented in the interface under
**Documentation → Routing rules**.

Ready-made rule sets can be imported from a URL instead of written by hand:
**Settings → RoutingA → Rule source**. The panel shows the address, the
version last served, and — when the index names several sets — a picker for
which set to import. An import replaces the rules, so the editor reloads and
shows what the source brought. Importing the same version twice answers that
nothing changed.

### DNS

**Settings → DNS Settings** lists the DNS servers with the domains each one
answers and the outbound each one uses (`direct`, `proxy`, or a group name).
Rules can likewise be imported from a URL (**Rule source** in the same
dialog). Queries are answered on port 52353; the transparent modes point the
system resolver there automatically.

### Ports and sharing

**Settings → Address and Ports** changes every inbound; `0` closes one. Only
the SOCKS5-with-rules inbound is on by default. Sharing the ports beyond
this machine (port sharing, LAN sharing) exposes an open proxy to the
network — protect it with a custom inbound carrying a username and password.

### Logs

The **Logs** page shows the service log; the core log is one toggle away.
When something breaks, reproduce it once and read the log around that
moment: every connection logs which outbound it took, and every DNS query
logs which upstream answered.

## Back up

Every setting — nodes, subscriptions, groups, rules, sources — lives in one
database file. Stop the service and copy it:

```sh
sudo systemctl stop v2raya
cp ~/.config/v2raya/v2raya.db ~/v2raya-backup-$(date +%F).db
sudo systemctl start v2raya
```

To restore, stop the service, copy the file back and start again. A package
upgrade does not touch the settings, but take a copy before it anyway.