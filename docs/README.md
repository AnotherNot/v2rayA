# Documentation

Documentation that lives in the repository rather than in the interface. The
in-app documentation, which the settings pages link to, is under
`gui/src/docs/` and is translated into the six locales.

Every document below exists in English and in Russian (`-ru.md` next to the
original). The in-app sections added for this checkout's features are written
in English and Russian; the remaining in-app locales fall back to English.

## Start here

- **[user-guide.md](user-guide.md)** ([на русском](user-guide-ru.md)) —
  install v2rayA, connect the first node, and run it day to day. No command
  line beyond installing. This is the document for the person using v2rayA,
  not for the person building it.
- **[running-from-source.md](running-from-source.md)**
  ([на русском](running-from-source-ru.md)) — build this checkout and
  prove a node connects and a rule set reaches the core, on a clean machine, in
  order. This is the entry point for anyone who has not run this code before.

## Feature documentation

- **[testing-on-a-real-machine.md](testing-on-a-real-machine.md)**
  ([на русском](testing-on-a-real-machine-ru.md)) — what each
  check proves and how to read a wrong result, for a machine that already runs
  v2rayA. Covers routing rules, rule sources, DNS sources, and the modes that
  change the host.
- **[redirect-bound-device.md](redirect-bound-device.md)**
  ([на русском](redirect-bound-device-ru.md)) — the opt-in
  REDIRECT bypass for sockets bound to a device, its preconditions, and the live
  validation it has had so far.

## A note on what is verified

The two testing documents carry a section listing what has **not** been
observed. Both were written from the code and from the service-level tests,
and both have since been run end to end by hand against a live v2rayA with
real nodes — twice, the second time with two nodes in a balancing group.
The observed sections record what each run established and what it found;
when the next run disagrees with them, they should be corrected against it.

Commands in them are taken from the code, so when a request body or a response
field changes, both documents need the same edit. The API they use is listed in
[testing-on-a-real-machine.md](testing-on-a-real-machine.md) section 9.

## What the code stands on

Nothing here was invented where an existing shape would do. When a format or
a mechanism below appears in the code, it is there because the source next
to it proved it first:

- **v2rayN** — the share-link dialect the import understands (`mode`,
  `x_padding_bytes` and the `extra` object on XHTTP links, the VLESS
  `encryption` parameter) and the shape of the published rule index the
  routing source reads (`template.json` with version, remarks and named
  sets). Where v2rayA diverges it does so on purpose: the DNS source is
  this project's own rule shape, not v2rayN's raw core-DNS template,
  because importing that would mean inventing which upstream answers which
  name (see [testing-on-a-real-machine.md](testing-on-a-real-machine.md)
  section 7.1).
- **Xray-core (XTLS)** — the core underneath `v2raya_core`: the routing
  model of inbound tags, outbound tags and balancers the generator writes,
  the `"min-max"` integer-range form the loader accepts, and the forced
  outbound tag (`SetForcedOutboundTagToContext`, the same mechanism xray
  uses to dial a tagged outbound) the DNS module dispatches through.
- **miekg/dns** — the DNS wire handling inside the `core/dns` module,
  including the listener semantics the best-effort extra addresses build on.
- **runetfreedom** — the published Russian rule sets
  (`russia-v2ray-custom-routing-list`) and the matching geo files
  (`russia-v2ray-rules-dat`) the recipe imports and swaps in; v2fly's stock
  files do not carry those categories.
- **v2fly / domain-list-community** — the default `geoip.dat` / `geosite.dat`
  a fresh install downloads, and the lookup convention (tags stored upper
  case, matched case-insensitively) the tag checks rely on.
- **RoutingA** (`github.com/v2rayA/RoutingA`) — the rule syntax the editor
  and the converter parse.
