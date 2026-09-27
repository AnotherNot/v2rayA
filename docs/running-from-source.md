# Running v2rayA from source, and proving it works

A recipe for a clean machine: build this checkout, start it, and prove that a
node connects and that a rule set reaches the core. Written to be followed in
order without reading anything else.

It is the entry point. For verifying one specific feature on a machine that
already runs v2rayA, see [testing-on-a-real-machine.md](testing-on-a-real-machine.md).

## What you need

- **Docker.** Every build step below runs in a container; nothing has to be
  installed on the host.
- **A node.** One share link — `vless://`, `vmess://`, `trojan://` or `ss://` —
  from a provider. There is no default; everything below depends on it.
- **About fifteen minutes.** The first build downloads dependencies.

**No root is needed.** The transparent proxy is off by default
(`service/db/configure/setting.go`), so v2rayA installs no firewall rules, does
not touch `/etc/resolv.conf` and opens no TUN device. It only listens on
loopback ports.

## The five ports

| Port | Tag in the core configuration | Receives routing rules |
| --- | --- | --- |
| 2017 | `api-in` | — |
| 20170 | `socks` | **no** |
| 20171 | `http` | **no** |
| 20172 | `rule-http` | **yes** |

This table is the single most common reason a rule set "does nothing": rules
are attached to the rule ports only
(`service/kernel/v2ray/template_routing.go`, `setRulePortRouting`). A client
pointed at 20170 or 20171 will never see them. There is also a SOCKS5-with-rules
inbound, off by default.

In a generated configuration the tags carry an address-family suffix, so a rule
reads `["rule-http_ipv4", "rule-socks", "rule-http_ipv6"]`. Match on the `rule-`
prefix rather than an exact name.

## Step 1 — build

```bash
cd ~/v2raya-test/src

docker run --rm -v "$PWD:/repo" -w /repo/gui node:24 \
  bash -c 'corepack enable >/dev/null && yarn install --frozen-lockfile && yarn build'

mkdir -p service/server/router/web && cp -a web/. service/server/router/web/

docker run --rm -v "$PWD:/repo" -w /repo/core golang:1.26 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya_core ./main'

docker run --rm -v "$PWD:/repo" -w /repo/service golang:1.26 \
  bash -c 'CGO_ENABLED=0 go build -trimpath -o /repo/v2raya .'

ls -la v2raya v2raya_core
```

**You should see** two files listed with sizes. If the first command fails, the
usual cause is no `web/` output, which every later step also needs.

## Step 2 — start

```bash
cd ~/v2raya-test/src
./v2raya --lite > /tmp/v2raya.log 2>&1 &

sleep 5
ss -lntp | grep -E '2017|20170|20171'
```

**You should see** three lines, one per port.

On the very first start v2rayA downloads `geoip.dat` and `geosite.dat`, about
90 MB together, and the API does not answer until that finishes. Wait a minute
or two, then:

```bash
curl -s http://127.0.0.1:2017/api/version | head -c 200; echo
```

**If nothing answers**, read `/tmp/v2raya.log` — the reason is in the first
lines. The flag `--lite` is implied when not running as root; it is given here
so the intent is on the record.

## Step 3 — an account, and keep the token

There are no accounts on a fresh install, so the API answers `first: true` and
refuses everything else until one exists.

```bash
curl -s -X POST http://127.0.0.1:2017/api/account \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"test1234"}'
```

Every later request needs the token. Store it so you never paste it by hand:

```bash
curl -s -X POST http://127.0.0.1:2017/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"test1234"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])' > /tmp/token

export TOKEN=$(cat /tmp/token)
echo "token: ${TOKEN:0:20}..."
```

**You should see** the start of a long string. `TOKEN` lives only in this shell;
in a new terminal run `export TOKEN=$(cat /tmp/token)` again.

## Step 4 — add a node and connect it

Put your own link where `<LINK>` is. The field is `url`, singular:

```bash
curl -s -X POST http://127.0.0.1:2017/api/import \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"<LINK>"}' > /dev/null

SERVER_ID=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["touch"]["servers"][0]["id"])')
echo "node id: $SERVER_ID"
```

Selecting the node is a separate call, and it must happen before the core will
start — `StartV2ray` refuses with `no server is selected`. **This call also
starts the core**, so the call after it only confirms the state:

```bash
curl -s -X POST http://127.0.0.1:2017/api/connection \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"_type\":\"server\",\"id\":$SERVER_ID,\"sub\":0,\"outbound\":\"proxy\"}" > /dev/null

curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/touch \
  | python3 -c 'import json,sys; c=json.load(sys.stdin)["data"]["touch"]["connectedServer"]; print("connected:", c)'
```

**You should see** `connected: [{'_type': 'server', ...}]`.

This request takes **no body**:

```bash
curl -s -X POST http://127.0.0.1:2017/api/v2ray -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys; print("running:", json.load(sys.stdin)["data"]["running"])'
```

**You should see** `running: True`.

## Step 5 — does the node work at all?

This step is not optional. Everything after it measures routing, and a broken
node looks exactly like a broken rule set.

```bash
echo "A, direct:  $(curl -s --max-time 15 https://ifconfig.me)"
echo "B, through: $(curl -s --max-time 15 --socks5-hostname 127.0.0.1:20170 https://ifconfig.me)"
```

**A and B must differ.** If they are equal, the node is not carrying traffic and
no later result means anything.

`--socks5-hostname` is required. Without it curl resolves the name locally and
sends an address, so `geosite:` rules can never match.

Keep A and B: every check from now on is "did this give A or B?".

## Step 6 — prove the routing mechanism works

Do this with a rule whose outcome you already know, before importing anything
published. Debugging a bad connection and a bad conversion at once is much
harder than proving each in turn.

```bash
cat > /tmp/rule.txt <<'EOF'
# control: example.com goes out directly, everything else through the proxy
domain(example.com)->direct
default: proxy
EOF

python3 -c 'import json;print(json.dumps({"routingA":open("/tmp/rule.txt").read()}))' > /tmp/rule.json
curl -s -X PUT http://127.0.0.1:2017/api/routingA \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/rule.json > /dev/null
```

Turn on RoutingA. The setting is `pacMode` in JSON, and the request body is the
setting object **itself** — not wrapped in a `{"setting": …}` object. Wrapping it
is silently ignored: the handler decodes the body over the stored setting, an
unknown key matches nothing, and `pacMode` stays where it was. Read, change one
field, write it back:

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:2017/api/setting \
  | python3 -c '
import json,sys
s=json.load(sys.stdin)["data"]["setting"]
s["pacMode"]="routingA"
print(json.dumps(s))' > /tmp/set.json

curl -s -X PUT http://127.0.0.1:2017/api/setting \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/set.json
```

**You must see** `"code": "SUCCESS"`. A `FAIL` here is the subject of step 7 and
is worth reading before going on.

**Now the check that matters most.** If no rule carries a rule-port tag,
RoutingA did not apply and every measurement below is meaningless:

```bash
CFG="$HOME/.config/v2raya/config.json"
python3 - "$CFG" <<'PY'
import json,sys
rules = json.load(open(sys.argv[1])).get("routing", {}).get("rules", [])
mine = [r for r in rules
        if any(t.startswith("rule-") for t in (r.get("inboundTag") or []))]
print("rules total:", len(rules), "| on rule ports:", len(mine))
for r in mine:
    print("  ", r.get("outboundTag") or r.get("balancerTag"),
          "<-", r.get("domain") or r.get("ip"))
PY
```

**You should see** `on rule ports: 2`, one `direct` and one `proxy`.

Finally, compare the two ports. This is the proof that rules are being applied:

```bash
echo "20172 (expect A): $(curl -s --max-time 15 -x http://127.0.0.1:20172 https://ifconfig.me)"
echo "20170 (expect B): $(curl -s --max-time 15 --socks5-hostname 127.0.0.1:20170 https://ifconfig.me)"
```

20172 must give **A** and 20170 must give **B**. If both give B, the rule did
not apply.

## Step 6b — install the geo files the rules need

**Do this before step 7, or step 7 breaks the service.**

A rule set is a list of names like `geosite:ru-blocked` and `geoip:ru-blocked`.
The core resolves those names against the geo files in the asset directory. v2rayA
installs v2fly's `geoip.dat` and `domain-list-community`'s `dlc.dat` on first run,
and **neither contains the Russian categories**. So:

- the import in step 7 succeeds and reports `SUCCESS`,
- the rules are stored,
- and then **the core refuses to start**, because it cannot resolve
  `geoip:ru-blocked`. v2rayA reports this as
  `invalid config: the core could not restart with the new settings, the previous
  ones are back` and silently rolls the setting back.

The detail is in the v2rayA log, not in the API response:

```bash
grep -i 'illegal ip rule\|Failed to start' "$(dirname "$(which v2raya)")/../.config/v2raya/v2raya.log" 2>/dev/null \
  || grep -i 'illegal ip rule\|Failed to start' /tmp/v2raya.log
```

```
illegal ip rule: geoip:ru-blocked > failed to check code RU-BLOCKED from geoip.dat > EOF
```

`EOF` there means "walked the whole file and did not find the code", not a
truncated download.

Install the matching files before importing. The asset directory is
`$XDG_DATA_HOME/v2raya`, which is where the core is told to look:

```bash
ASSETS="${XDG_DATA_HOME:-$HOME/.local/share}/v2raya"
echo "каталог ассетов: $ASSETS"

BASE='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release'
for f in geoip.dat geosite.dat; do
  [ -f "$ASSETS/$f" ] && cp "$ASSETS/$f" "$ASSETS/$f.v2fly.bak"
  curl -sSL -o "$ASSETS/$f" "$BASE/$f" && echo "$f: $(stat -c%s "$ASSETS/$f") байт"
done
```

**You should see** roughly 18 MB and 74 MB. The backups are named `.v2fly.bak`;
restore them to go back:

```bash
cd "$ASSETS" && for f in geoip.dat geosite.dat; do [ -f "$f.v2fly.bak" ] && mv "$f.v2fly.bak" "$f"; done
```

Two consequences worth knowing:

- The core caches nothing between starts, so the swap takes effect on the next
  configuration write. Re-send step 6's `PUT /api/setting` afterwards.
- A replaced `geosite.dat` is safe: the Russian file is a superset of
  `domain-list-community`, so the built-in whitelist mode keeps working. A
  replaced `geoip.dat` is **not** a drop-in for every use — it lacks the
  country categories the built-in modes reference (`cn`, `private`, `hk`, `mo`).
  That is why both files must come from the same source.

## Step 7 — import a published rule set

```bash
RULES='https://raw.githubusercontent.com/runetfreedom/russia-v2ray-custom-routing-list/main/v2rayN/template.json'

python3 -c "import json;print(json.dumps({'url':'$RULES','remarks':'Заблокированное'}))" > /tmp/imp.json
curl -s -X PUT http://127.0.0.1:2017/api/routingSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/imp.json \
  | python3 -m json.tool
```

**You should see** `alreadyUpToDate: false` and a `version`.

The set is called "Заблокированное" because it is the most revealing of the
three: it uses `proxy`, `block` and `direct` together. In the other two almost
everything falls through to the default, so a misconfiguration is much easier
to miss.

Confirm the rules arrived, and check that the tags they name really are in the
files on this machine:

```bash
python3 - "$CFG" <<'PY'
import json,sys
rules = json.load(open(sys.argv[1])).get("routing", {}).get("rules", [])
mine = [r for r in rules
        if any(t.startswith("rule-") for t in (r.get("inboundTag") or []))]
print("rules on rule ports:", len(mine))
PY

ASSETS="${XDG_DATA_HOME:-$HOME/.local/share}/v2raya"
python3 - "$ASSETS/geosite.dat" "$ASSETS/geoip.dat" <<'PY'
import sys
for path, tags in ((sys.argv[1], ("ru-blocked","category-ads-all","private","cn")),
                   (sys.argv[2], ("ru-blocked","private","cn","hk","mo"))):
    raw = open(path,'rb').read().decode('latin-1')
    print(path.rsplit('/',1)[-1], {t: (t.upper() in raw) for t in tags})
PY
```

**You should see** `rules on rule ports: 9` and no `False`. A `False` here is
what step 6b exists to prevent.

## Step 8 — check each branch

Two values from step 5: **A** is the direct exit, **B** is the node's.

```bash
P="-x http://127.0.0.1:20172 --max-time 10"

# geoip:private -> direct: should answer fast and give A
curl -s $P http://192.168.0.1/ -o /dev/null -w 'private: %{http_code}\n'
curl -s $P https://ifconfig.me; echo

# geosite:category-ads-all -> block: the connection should fail fast
curl -s $P https://ads.example/ -o /dev/null -w 'ads: %{http_code}\n'

# geosite:ru-blocked -> proxy: should give B
curl -s $P https://<a-domain-from-the-list>/ -o /dev/null -w 'blocked: %{http_code}\n'
curl -s $P https://ifconfig.me; echo
```

| What you see | What it means |
| --- | --- |
| `private: 000` after 1–2 seconds | the rule fired, the traffic went direct |
| `ads: 000` after 1–2 seconds | blocking works |
| `blocked: 200` and the IP is **B** | blocked domains go through the node |
| `000` after the full 10 seconds | the rule did **not** fire; traffic went the usual way |
| the IP is **A** where **B** was expected | the downloaded file has no such tag |

To pick a domain for the last line, read the tags of the file this machine
downloaded:

```bash
ASSET=$(find "$HOME/.config" /var/lib /usr/local/share -name geosite.dat 2>/dev/null | head -1)
python3 -c "
raw = open('$ASSET','rb').read().decode('latin-1')
for t in ('ru-blocked','category-ads-all','private','cn'):
    print(f'{t:20}', 'present' if t.upper() in raw else 'ABSENT')"
```

## Step 9 — the same, from the interface

Everything above is reachable without a terminal. Open
**http://127.0.0.1:2017**, sign in with the account from step 3, then:

- **Settings → Advanced Settings → RoutingA** — the rule source panel: the URL,
  the version it last served, and a picker for which set to import. An import
  replaces the rules, so the editor reloads and shows what the source brought.
- **Settings → DNS Settings** — the same panel for DNS rules.

## Step 10 — DNS rules from a source (optional)

A DNS source publishes rules in this project's own shape, one upstream and one
outbound each. It is **not** the format v2rayN exchanges, for a reason given in
[testing-on-a-real-machine.md](testing-on-a-real-machine.md) section 7.1.

```bash
cat > /tmp/dns.json <<'EOF'
{
  "version": "my-dns-1",
  "remarks": "my set",
  "rules": [
    { "upstream": "127.0.0.1", "domain": "geosite:private", "outbound": "direct" },
    { "upstream": "1.0.0.1", "outbound": "proxy" }
  ]
}
EOF

python3 -m http.server 8899 --directory /tmp > /dev/null 2>&1 &
sleep 1

python3 -c 'import json;print(json.dumps({"url":"http://127.0.0.1:8899/dns.json"}))' > /tmp/dnssrc.json
curl -s -X PUT http://127.0.0.1:2017/api/dnsSource \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @/tmp/dnssrc.json \
  | python3 -m json.tool
```

**You should see** `alreadyUpToDate: false`. The rules land in the generated
configuration under `dns_module`, not under `routing`. Whether the module then
answers is a separate question — see the open item at the end; verify with
`dig @127.0.0.1 -p 52353 example.com` before trusting it:

```bash
python3 - "$CFG" <<'PY'
import json,sys
dns = json.load(open(sys.argv[1])).get("dns_module") or {}
print("module keys:", sorted(dns.keys()))
for r in dns.get("rules", []):
    print("  ", r.get("action","route"), r.get("outbound","direct"), "<-", r.get("domain","*"))
PY
```

## When something goes wrong

| Symptom | Cause | What to do |
| --- | --- | --- |
| 2017 does not answer on the first start | geo files are downloading, ~90 MB | wait two minutes, then `tail /tmp/v2raya.log` |
| Step 5: A equals B | the node carries no traffic | check `running: True` in step 4, then the log |
| `no server is selected` | step 4's connect call was skipped | run the connect call, then start the core again |
| `curl: (7) Failed to connect` on 20172 | the rule port is not in RoutingA mode | `pacMode` must be `routingA`, see step 6 |
| `pacMode` is still the old value after the PUT | the body was wrapped in `{"setting": …}` | the body is the setting object itself, see step 6 |
| `on rule ports: 0` | rules did not apply | repeat step 6 and confirm `pacMode` |
| `invalid config: the core could not restart` | a rule names a tag the geo files do not have | step 6b; the detail is in the log |
| 401 on any request | the token is stale or changed | `export TOKEN=$(cat /tmp/token)` |
| `REQUEST_IN_PROGRESS` | another mutation is running | wait five seconds and retry |
| Rules "sort of work" on 20170 | they are not there at all | use **20172** |
| The import answers `ROUTING_RULES_INCOMPATIBLE` | the set matches on process name, which RoutingA cannot express | pick a different set, or file the omission upstream |

The two steps that must not be skipped are **step 5** and the configuration
check in **step 6**. Without them a dead node is indistinguishable from a rule
set that does not work.

## Cleaning up

```bash
pkill -f 'v2raya --lite'
rm -rf "$HOME/.config/v2raya"     # this removes every setting
```

## What has been observed, and what has not

Steps 1 to 10 were run on a Linux host on 2026-09-27 against a build of this
checkout, with a working VLESS node (xhttp+tls). The run found three bugs, all
fixed and covered by tests; the fixes are what this checkout carries.

**The run, in order.** The build sequence works as written. The API bodies are
as documented, except `PUT /api/setting`, whose body is the setting object
itself — a `{"setting": …}` wrapper is silently ignored (found that way, after
the mode appeared not to take effect). Selecting a node starts the core, so the
later start call only confirms. A versus B differ, and both are stable across
repeated requests. The control rule `domain(ifconfig.me)->direct` on port 20172
returns the direct egress while the default returns the node's, which proves
the mechanism before any import.

Importing the published index and the "Заблокированное" set over the network
succeeds and reports `version: RUv1`, with eleven rules on the rule ports (nine
from the set, the Apple push rule, and the converter's default line).
`domainStrategy: IPOnDemand` is carried from the set into the configuration.
A repeated import answers `alreadyUpToDate: true`.

**Branch by branch, correlated with the core log.** The core logs
`taking detour [X]` per connection, so each outcome below was read from the log,
not inferred from timing:

| Destination | Detour | Observed |
| --- | --- | --- |
| 192.168.0.1:80 | direct (geoip:private) | fast refusal |
| 4pda.to:443, a member of geosite:ru-blocked in the downloaded file | proxy | answered through the node (HTTP 403 from the site) |
| an ads domain from geosite:category-ads-all | block | connection killed in ~0.01 s |
| neverssl.com:80 | direct (trailing rule) | fast refusal |

The SOCKS5-with-rules port (20173, enabled for the check) routes through the
same set: an unmatched domain returns the direct egress, as the trailing rule
says. The rule-source fetch itself was seen in the core log going through the
local SOCKS inbound into the proxy outbound
(`accepted tcp:raw.githubusercontent.com:443 [socks_ipv4 -> proxy]`), which
proves the proxy download path.

**The interface was driven, not mocked.** Playwright (Chromium, headless)
against the live backend: login, dismiss the first-run tutorial, open Settings,
open the RoutingA dialog. The panel shows the URL, the set picker on
"Заблокированное", `Version: RUv1` and the domain strategy; the text editor
holds the imported rules (`# RUv1-Заблокированное`, `protocol('bittorrent')`,
`geosite:ru-blocked`). The DNS dialog shows its panel. Opening the node from
the Proxies page via Modify and saving it unchanged keeps the `encryption`
parameter on the share link and the node keeps working — this exercises both
codecs end to end. The specs live next to the test session, not in the
repository; component tests cover the same components against mocks.

**Four bugs this run found.** All four were bisected live, fixed, and covered
by unit tests plus the live re-run:

1. **VLESS links with `encryption` were silently broken.** The parser ignored
   the parameter and wrote `"none"`, so the tunnel established and the server
   dropped every stream without a sound. Fixed in both parsers (Go and the
   GUI codec), with `mode`, `x_padding_bytes` and the `extra` object accepted
   as well. Without this fix nothing below step 5 can work for such links.
2. **Every non-zero XHTTP range stopped the core.** `XHTTPRangeConfig`
   serialized as `{"from":..,"to":..}`, while the core's loader accepts only
   `"min-max"` or a plain integer. This broke padding from an import — and
   would have broken any GUI-set range too. The struct now marshals as
   `"100-1000"`.
3. **A non-root service could ask the DNS module for port 53.** The generator
   adds the `127.2.0.17:53` listener when the port looks free, but free is not
   bindable without privilege, and then the core exits on start. The generator
   now skips the listener without root, and the core's listener treats extra
   addresses as best-effort: one it cannot bind is skipped with a warning
   instead of failing the whole module.
4. **The DNS module resolved "through the proxy" directly.** The dispatcher
   adapter set the proxy tag as an *inbound* tag, which matches no routing
   rule, so the query fell through to the default route — direct. Plain UDP
   and DoH upstreams with `outbound: proxy` both answered SERVFAIL (direct
   UDP is filtered in that sandbox, direct TCP/53 is poisoned), while curl
   through the SOCKS inbound worked. The adapter now forces the outbound
   directly on the dispatch context (`SetForcedOutboundTagToContext`, the same
   mechanism xray uses to dial a tagged outbound), bypassing the rules.
   Balancer group tags have no outbound handler to force, so the generator
   emits them as `group_tags` and the module sends those upstreams through
   the local SOCKS inbound, where the normal routing — balancers included —
   applies. The SOCKS path also learned `https` (DoH POST over TLS) and
   `tls`, which it previously attempted as plain DNS-over-TCP.

What that run did **not** establish:

- the transparent modes and Port Sharing, which change the host;
- the DNS source against a real remote URL; the recipe serves a local file;
- the daily source ticks firing; only the code path behind them is covered.

### Groups verified live with two nodes

A later run on 2026-09-27 imported a second VLESS node (same host, other
gateway) and moved both into a group `eu` (leastping, the default). What that
run established, backend and interface:

- the generator emits `group_tags: ["eu", "proxy"]` — both groups hold both
  members, so both are balancers — with member outbounds tagged per node;
- a DNS rule with `outbound: eu` answers through the group: the core log shows
  the query on the local SOCKS inbound into the balancer's pick
  (`accepted tcp:1.0.0.1:53 [socks_ipv4 -> <EU member>]`), never a forced
  detour — the group path, not the single-outbound one;
- traffic with `default: eu` flows (12/12 HTTP 200) and lands on the faster
  member every time, as leastping should;
- `PUT /api/outboundSelection` pins the group to node 1 (`taking detour [eu]`,
  still 200) and `which: null` returns it to balancing (member tag back in
  the log);
- Playwright, same session: the Proxies page shows both node cards; the
  dashboard's group menu opens the EU settings (probe URL, 60 s, Least
  Latency First); the DNS dialog offers `eu` next to `direct`, and switching
  the catch-all to `eu` and back through the GUI sticks on the backend both
  ways; saving the second node unchanged keeps its `encryption` parameter.
  Seven specs green in total.

### Closed: the DNS module answers through the proxy

The previously open item is root-caused and fixed (bug 4 above). Verified
live on 2026-09-27 with the VLESS node: with the default rules, `dig
@127.0.0.1 -p 52353 example.com` falls to the `1.0.0.1` upstream through the
proxy and answers with the unpoisoned addresses, and the core log shows
`taking platform initialized detour [proxy] for [tcp:1.0.0.1:53]`. With a DoH
rule (`https://1.0.0.1/dns-query`, `outbound: proxy`) the same query answers
as well, via `taking platform initialized detour [proxy] for
[tcp:1.0.0.1:443]`. The sandbox's UDP egress was never the problem — the
module simply never dialled through the proxy.
