# Testing v2rayA on a real machine

A procedure for verifying on a live host what the automated tests can only
approximate: that a node connects, that a rule set reaches the core, and that
traffic actually takes the branch the rules name.

Parts of this document have been run against a build of this checkout; the last
section says which parts and which have not. Every command is derived from the
code and from the service-level tests in
`service/server/service/routingsource_smoke_test.go` and `dnssource_test.go`,
which exercise the full download-convert-store path against the published
documents served locally. Update this document when the feature set changes, and
record what was actually observed in the last section.

For a shorter path, and the one to follow first, see
[running-from-source.md](running-from-source.md): build, launch, and prove a node
connects. This document is the longer version — what each check proves and how to
read a wrong result.

## Before you start

### The default mode does not touch the machine

With `transparent: close` — the default (`service/db/configure/setting.go`) —
v2rayA installs no firewall rules, does not touch `/etc/resolv.conf` and opens
no TUN device. The whole procedure below therefore runs **without root and
without a single change to the host's network**.

This is not a test mode; it is the ordinary "explicit proxy" mode. An
application points at SOCKS, and v2rayA carries the traffic into VLESS.

### Which ports carry rules

| Port | Core tag | Receives RoutingA rules |
| --- | --- | --- |
| 20170 | `socks` | no |
| 20171 | `http` | no |
| 20172 | `rule-http` | yes |
| `socks5WithPac` | `rule-socks` | yes |

Rules are attached to `rule-http` and `rule-socks` only
(`service/kernel/v2ray/template_routing.go`, `setRulePortRouting`). A client
pointed at 20170 or 20171 will not see an imported rule set. This is the most
likely reason for "the rules do nothing".

`socks5WithPac` defaults to `0`, meaning the SOCKS5-with-rules inbound is off
until you enable it.

### Prove the mechanism before trusting an import

Do these in order. Debugging a connection failure and a conversion failure at
once is much harder than proving each separately.

1. Show the node works: a direct request and a request through port 20170 return
   different egress addresses.
2. Write one rule whose outcome you already know and check it.
3. Only then import a published rule set.

## 1. Build and launch

### 1.1 Build

```sh
docker run --rm -v "$PWD:/repo" -w /repo/gui node:24 \
  bash -c 'corepack enable >/dev/null && yarn install --frozen-lockfile && yarn build'
mkdir -p service/server/router/web && cp -a web/. service/server/router/web/
docker run --rm -v "$PWD:/repo" -w /repo/core golang:1.27 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya_core ./main'
docker run --rm -v "$PWD:/repo" -w /repo/service golang:1.27 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya .'
```

`./build.sh` does the same with a local Go 1.27 and yarn.

The `-v "$PWD:/repo"` mount is what makes local changes build: the
container sees the working tree, uncommitted edits included, so there is no
separate step to "pick up" modifications — edit, rerun the commands above,
restart the service.

If `docker run` fails after pulling the image with
`failed to add the host <=> sandbox pair interfaces: operation not
supported`, the host cannot create the bridge network's virtual interfaces
and the container never starts. Share the host's network stack instead, on
all three commands:

```sh
docker run --rm --network host -v "$PWD:/repo" -w /repo/gui node:24 \
  bash -c 'corepack enable >/dev/null && yarn install --frozen-lockfile && yarn build'
```

Without Docker at all, the same commands run natively with a local Node 24
(+ yarn, `corepack enable` first) and a local Go 1.27 — from the repository
root, in `gui`, `core` and `service` in that order, keeping the
`mkdir`/`cp` step between the interface and the service.

### 1.2 Launch without touching the system

```sh
./v2raya --lite
```

Without root the configuration lives under `$XDG_CONFIG_HOME/v2raya`
(`service/conf/environmentConfig_other.go`); the generated core configuration is
`<config dir>/config.json`.

```sh
curl -s http://127.0.0.1:2017/api/version | head -c 400
ss -lntp | grep -E '2017|20170|20171|20172'
```

### 1.3 First run: an account

The API answers `first: true` until an account exists.

```sh
curl -s -X POST http://127.0.0.1:2017/api/account \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"CHANGE_ME"}'

TOKEN=$(curl -s -X POST http://127.0.0.1:2017/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"CHANGE_ME"}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
```

Every later request needs `Authorization: Bearer $TOKEN`.

## 2. Node and the control measurement

Add a node, select it, and start the core. The import body names the link under
`url`, singular, and selecting the node is a separate call that must happen
before the core will start — `StartV2ray` refuses with `no server is selected`.

```sh
curl -s -X POST http://127.0.0.1:2017/api/import \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"vless://<uuid>@host:443?security=tls&sni=example.com&type=ws#my-node"}'

SERVER_ID=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["touch"]["servers"][0]["id"])')

curl -s -X POST http://127.0.0.1:2017/api/connection \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"_type\":\"server\",\"id\":$SERVER_ID,\"sub\":0,\"outbound\":\"proxy\"}"

# this request takes no body
curl -s -X POST http://127.0.0.1:2017/api/v2ray -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys; print("running:", json.load(sys.stdin)["data"]["running"])'
```

The whole sequence is in [running-from-source.md](running-from-source.md) step 4.

Then measure the two addresses everything else is compared against:

```sh
curl -s https://ifconfig.me; echo                                   # A: this host
curl -s --socks5-hostname 127.0.0.1:20170 https://ifconfig.me; echo # B: the node
```

`--socks5-hostname` (curl's `socks5h`) hands the **domain** to the proxy.
Without it curl resolves locally and sends an address, and the core never sees
a name, so `geosite:` rules cannot match.

If A equals B the node is not working; nothing below will tell you anything.

## 3. Prove that routing rules reach the core

### 3.1 Write a rule with a known outcome

```sh
cat > /tmp/known.txt <<'EOF'
# control rule: example.com goes out directly, everything else through the proxy
domain(example.com)->direct
default: proxy
EOF

python3 -c 'import json; print(json.dumps({"routingA": open("/tmp/known.txt").read()}))' > /tmp/r.json
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/r.json
```

### 3.2 Turn on RoutingA

`pacMode` is the JSON name of `RulePortMode` (`service/db/configure/setting.go`).
`PUT /api/setting` takes the whole object, so read, change, write:

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["pacMode"]="routingA"
print(json.dumps(s))' > /tmp/s.json
curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/s.json
```

### 3.3 Confirm the rules are in the core configuration

This is the check that matters most. If no rule carries `inboundTag: rule-*`,
RoutingA was not applied and every measurement below is meaningless.

```sh
CFG="${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/config.json"
python3 - "$CFG" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))
rules=c.get("routing",{}).get("rules",[])
print("rules:",len(rules))
for r in rules:
    tags=r.get("inboundTag") or []
    if any(t.startswith("rule-") for t in tags):
        what=r.get("domain") or r.get("ip") or r.get("port") or r.get("network")
        print(" ",r.get("outboundTag") or r.get("balancerTag"),"<-",what)
PY
```

### 3.4 Measure

```sh
P="-x http://127.0.0.1:20172"
curl -s $P https://example.com     -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo    # expect A
curl -s $P https://api.github.com -o /dev/null -w '%{http_code}\n'
```

A `-> direct` rule must yield A, a `-> proxy` rule must yield B. If both yield
B, the rule did not apply.

## 4. Import a published rule set

> **Install the geo files the rules name, first.** v2rayA fetches v2fly's
> `geoip.dat` and `domain-list-community`'s `dlc.dat`, and the published Russian
> categories are in neither. The import below will report `SUCCESS` and store the
> rules, and **then the core will refuse to start** with
> `illegal ip rule: geoip:ru-blocked > failed to check code RU-BLOCKED from
> geoip.dat > EOF` — the v2rayA log has the detail, the API reports only
> `invalid config: the core could not restart with the new settings, the previous
> ones are back`, and the setting is rolled back silently. `EOF` there means the
> code was not found, not that the download was truncated. Step 6b of
> [running-from-source.md](running-from-source.md) has the commands, and the tag
> check in 4.6 below is the thing to run before enabling RoutingA.

### 4.1 The source

```sh
RULES_URL='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-custom-routing-list/main/v2rayN/template.json'
```

### 4.2 Read the index first

```sh
python3 -c "import json;print(json.dumps({'url':'$RULES_URL'}))" > /tmp/src.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/src.json
```

`GET /api/routingSource` returns `items[]`, the names behind the source. The
published index offers "Всё", "Всё, кроме РФ" and "Заблокированное".

Start with **"Заблокированное"**. It is the most revealing set because it uses
`proxy`, `block` and `direct` together, and because everything else falls
through to `default: proxy` — in the other two sets a misconfiguration is much
easier to miss.

### 4.3 Import

```sh
python3 -c "import json;print(json.dumps({'url':'$RULES_URL','remarks':'Заблокированное'}))" > /tmp/imp.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json
```

Expect `{"alreadyUpToDate": false, "version": "RUv1", "remarks": "Заблокированное"}`.

### 4.4 What should be in the core configuration

Repeat 3.3. Nine rules, in this order:

```
block      <- domain(geosite:category-ads-all)
direct     <- protocol(bittorrent)
direct     <- ip(geoip:private)
direct     <- domain(geosite:private)
proxy      <- ip(1.0.0.1, 1.1.1.1, 8.8.8.8, 8.8.4.4)
proxy      <- network(udp) + port(50000-65535)
proxy      <- ip(geoip:ru-blocked)
proxy      <- domain(geosite:ru-blocked)
direct     <- port(0-65535)
```

The converter names the set in a comment (`# RUv1-Заблокированное`); comments
do not reach the configuration.

### 4.5 Check branch by branch

```sh
P="-x http://127.0.0.1:20172"

# geoip:private -> direct. Must return A, and must not stall.
curl -s $P --max-time 10 http://192.168.0.1/ -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo

# geosite:category-ads-all -> block. The connection must fail fast.
curl -s $P --max-time 10 https://ads.example/ -o /dev/null -w '%{http_code}\n'

# geosite:ru-blocked -> proxy. Must return B.
curl -s $P --max-time 10 https://<a-domain-from-the-list>/ -o /dev/null -w '%{http_code}\n'
curl -s $P https://ifconfig.me; echo

# the trailing port(0-65535) -> direct. Must return A, and must not stall.
curl -s $P --max-time 10 http://neverssl.com -o /dev/null -w '%{http_code}\n'
```

A rule that fires looks like a fast refusal or a fast return of A. A rule that
did **not** fire typically shows as a connection that sits until the timeout,
because traffic went out the proxy's own way.

### 4.6 Confirm the tags exist in the downloaded file

If 4.5's third case returns A instead of B, the tag is probably absent from the
file that was downloaded.

```sh
ASSETS=$(find "${XDG_CONFIG_HOME:-$HOME/.config}" /var/lib /usr/local/share -name geosite.dat 2>/dev/null | head -1)
echo "file: $ASSETS"
python3 - "$ASSETS" <<'PY'
import sys
raw=open(sys.argv[1],'rb').read().decode('latin-1')
for t in ('ru-blocked','category-ads-all','private','cn'):
    print(f"{t:20} {'present' if t.upper() in raw else 'ABSENT'}")
PY
```

The `release` files of `runetfreedom/russia-v2ray-rules-dat` were checked by
parsing them with the same protobuf types the service uses: `geosite.dat`
carries 1543 entries and `geoip.dat` 266, and both hold every tag the shipped
sets reference. Tags are stored upper case; the core looks them up with
`strings.EqualFold`, so case does not matter.

This is a check of the *files*, not of your installation. A `False` here means
the core will not start with the imported rules; install the matching files
first.

## 5. Idempotence, update, rollback

### 5.1 A repeated import changes nothing

```sh
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json
```

Expect `"alreadyUpToDate": true`. The point of the check: rules you edited by
hand are not overwritten, because the import compares what is stored against
what the source would produce.

### 5.2 Roll back

```sh
curl -s -X DELETE http://127.0.0.1:2017/api/routingSource -H "Authorization: Bearer $TOKEN"
```

The rules stay in place on purpose — a user who imported a set should not be
left with none. To go back to the built-in template, clear the rules:

```sh
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"routingA":""}'
```

An empty value falls back to the built-in template
(`service/db/configure/configure.go`, `GetRoutingA`).

### 5.3 Full reset

```sh
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/v2raya.db"*   # this is every setting
```

## 6. Fetching the source through the proxy

`proxyModeWhenSubscribe` defaults to `direct`, so **by default the source is
fetched without the proxy**. When the host cannot reach the source directly,
change it.

The proxy client is used only when the core is running **and** servers are
connected **and** the transparent proxy is off **and** the mode is `proxy` or
`pac` (`service/common/httpClient/httpClient.go`, `GetHttpClientAutomatically`).

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["proxyModeWhenSubscribe"]="proxy"
print(json.dumps(s))' > /tmp/sp.json
curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/sp.json
```

Watch the log to confirm which path was taken:

```sh
tail -f "${XDG_CONFIG_HOME:-$HOME/.config}/v2raya/v2raya.log" | grep -iE 'routing rules|Downloading'
```

**A common mistake:** importing while the core is not yet running. The client
is then `http.DefaultClient` and the fetch is direct. The order is: connect a
node, start the core, set `proxyModeWhenSubscribe`, then import.

## 7. DNS rules from a source

The routing source and the DNS source are separate: a routing source publishes
rules in the v2rayN format, a DNS source publishes them in this project's own
shape. Section 7.1 says why the two formats differ.

### 7.1 The DNS source format

The accepted document is either a versioned object:

```json
{
  "version": "RUdns1",
  "remarks": "Россия",
  "rules": [
    { "upstream": "127.0.0.1", "domain": "geosite:private", "outbound": "direct" },
    { "upstream": "77.88.8.8", "domain": "geosite:cn", "outbound": "direct" },
    { "upstream": "1.0.0.1", "outbound": "proxy" }
  ]
}
```

or a bare array of the same rule objects, in which case the version is taken
from a hash of the content, so an unchanged file is still recognised as
unchanged.

A rule with no upstream is refused: the generator drops such a rule, which would
turn a typo in a published document into queries nothing answers. An `action`
outside `route`, `reject` and `bypass` is refused and named in the error,
because accepting it would store a rule the generator ignores.

**What this deliberately does not accept.** v2rayN's DNS template is a
different thing: a raw core DNS object (`hosts` plus a flat `servers` list)
reached through a wrapper, with the per-domain mapping living inside a
configuration fragment. The published Russian template carries no per-domain
rules and no outbound assignment at all — its servers are bare strings.
Importing it would mean inventing which upstream answers which name, which is
the one thing a DNS configuration must never be guessed at. Use a document in
the shape above instead.

### 7.2 Import and check

```sh
DNS_URL='https://example.com/dns_rules.json'
python3 -c "import json;print(json.dumps({'url':'$DNS_URL'}))" > /tmp/dnsrc.json
curl -s -X PUT http://127.0.0.1:2017/api/dnsSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/dnsrc.json
```

Confirm the rules reached the core configuration. They live under `dns_module`,
not under `routing`: the module carries `upstreams`, `rules`,
`default_upstream`, `bootstrap`, `proxy_map` and `cache`.

```sh
python3 - "$CFG" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))
dns=c.get("dns_module") or {}
print("module keys:", sorted(dns.keys()))
for r in dns.get("rules", []):
    print(" ", r.get("action","route"), r.get("outbound","direct"), "<-", r.get("domain","*"))
print("upstreams:", len(dns.get("upstreams", [])), "default:", dns.get("default_upstream"))
PY
```

### 7.3 Idempotence and rollback

The same guarantees as for routing: a repeated import answers
`alreadyUpToDate: true`, a daily tick re-reads the source only when the version
moved, and forgetting the source keeps the rules.

```sh
curl -s -X DELETE http://127.0.0.1:2017/api/dnsSource -H "Authorization: Bearer $TOKEN"
```

### 7.4 In the interface

Both sources are managed from the dialog that owns the rules: **Settings →
Advanced Settings → RoutingA** for the routing source, **Settings → DNS
Settings** for the DNS one. The panel shows the URL, the version the source last
served and — for the routing source, whose index names several sets — a picker
for which set to import. An unconfigured source shows its form open.

An import replaces the rules, so the dialog reloads them and shows what the
source brought rather than what was open before.

## 8. Beyond the default mode

Each item below changes the host. Do not start here until sections 1–7 are
green: a broken rule set combined with a transparent mode sends traffic
nowhere, and you will not be able to tell which of the two is at fault.

### 8.1 SOCKS5 with rules

```sh
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/ports \
  | python3 -c '
import json,sys
p=json.load(sys.stdin)["data"]["ports"]
p["socks5WithPac"]=20173
print(json.dumps(p))' > /tmp/ports.json
curl -s -X PUT http://127.0.0.1:2017/api/ports \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/ports.json

curl -s --socks5-hostname 127.0.0.1:20173 https://ifconfig.me; echo
```

### 8.2 Transparent mode

`redirect` and `tproxy` install firewall rules, `tun` opens a TUN device, and
DNS interception is part of all of them. Prerequisites are root, iptables or
nftables, and for `tun` a `/dev/net/tun` device. The rules apply regardless of
the mode, so verify them through a rule port first.

### 8.3 Port sharing

Enabling `portSharing` moves the built-in inbounds onto all interfaces. The
built-in SOCKS and HTTP inbounds take **no password**
(`service/kernel/v2ray/templateJson.go`). On a network you do not trust that is
an open proxy on your node; use a custom inbound with a username and password.

## 9. API reference

| Method | Path | Body | Notes |
| --- | --- | --- | --- |
| GET | `/api/routingSource` | — | `{url, version, remarks, domainStrategy, items[]}`; the index is fetched live and its absence is not an error |
| PUT | `/api/routingSource` | `{url, remarks?}` | imports, restarts the core; empty `remarks` takes the first set |
| DELETE | `/api/routingSource` | — | forgets the source, keeps the rules |
| GET | `/api/dnsSource` | — | `{url, version, remarks}` |
| PUT | `/api/dnsSource` | `{url}` | imports DNS rules, restarts the core |
| DELETE | `/api/dnsSource` | — | forgets the source, keeps the rules |
| GET | `/api/setting` | — | `pacMode` selects `routingA`; `proxyModeWhenSubscribe` selects how sources are fetched |
| GET | `/api/touch` | — | current nodes and connections |

## 10. Automated coverage

```sh
cd service
go test -race ./server/service -run 'TestConvert|TestImportRoutingSource|TestSyncRoutingSource|TestImportDnsSource|TestSyncDnsSource'
```

The service tests convert all three published sets and parse the result with
`RoutingA.Parse`, then run the whole fetch-convert-store path against a local
server serving those same files. They do not start a core, open a socket, or
touch the host's network, so they cannot stand in for this procedure.

The interface side:

```sh
cd gui
yarn test src/dialogs/settings/RuleSource.spec.ts src/dialogs/settings/Dns.spec.ts
```

## 11. What has not been observed

The full run against a build of this checkout with a working node is recorded
in the "What has been observed" section of
[running-from-source.md](running-from-source.md): A-versus-B, the mechanism
proof, the import, all four branches with the core log as the oracle, the proxy
download path, and the interface driven by Playwright. That run also found three
bugs (dropped VLESS `encryption`, range objects the core rejects, the port-53
listener without root), all fixed and covered. What remains unobserved is below.

- **Sections 8.2 and 8.3 change the host** and have not been tried in this
  configuration. Firewall rules, the resolver file and TUN are the parts of
  v2rayA most likely to behave differently from a reading of the code.
- **A DNS rule with a balancer-group outbound** has been tried against a live
  two-node group since: the query goes through the local SOCKS inbound into
  the balancer's pick, and the GUI round-trips the group tag both ways; see
  the groups section in [running-from-source.md](running-from-source.md).
  Plain and DoH upstreams through the proxy were verified live as well.
- **No published DNS rule set exists in the shape section 7.1 describes.** The
  tests use documents written for them. The import path is proven, but nothing
  has been imported from a real remote DNS source.
- **The interface was driven by Playwright, not opened by a person.** The three
  specs (routing panel, DNS panel, editor save round-trip) assert the contract;
  a visual pass over both dialogs is still owed.
- **The daily source ticks** (`conf.TickerUpdateRoutingSource`,
  `conf.TickerUpdateDnsSource`) were not observed firing; only the code path
  behind them is covered.
