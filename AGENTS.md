# AGENTS.md

Instructions for a coding assistant working in this repository. A human owns every change you produce: your output is a draft until they have read it, run it, and can answer for it. [CONTRIBUTING.md](CONTRIBUTING.md) applies to you as written.

## What the repository is

`service/` is the Go service (API, settings database, transparent proxy, the core config it generates). `core/` is `v2raya_core`, a fork of xray-core carrying the DNS module, the in-core TUN and the extra protocols. `gui/` is the web interface: Vue 3, Vuetify 4 on Material 3, vue-i18n, Vitest. `install/` is packaging. The service embeds the built web files.

## Before you change anything

Read the code you are about to change and the code that calls it. Reuse the components and helpers that exist and follow the neighbouring file's conventions. Do not introduce a parallel implementation or a second convention.

## What the change must satisfy

```sh
./build-all.sh
```

That is the whole build: it runs the interface build in `node:24` and both Go
binaries in `golang:1.27`, leaves `./v2raya` and `./v2raya_core` in the
repository root, and keeps the host free of toolchains. It needs git and a
running Docker daemon, nothing else. The three toolchains separately, when you
want only one of them:

```sh
(cd gui     && yarn lint && yarn typecheck && yarn i18n-check && yarn test && yarn build)
(cd service && go build ./... && go vet ./... && go test ./...)
(cd core    && go build ./... && go vet ./...)
```

Run them yourself, from the repository root, and then exercise the change: for the service, start `v2raya` and use the feature; for the interface, open the page and use what changed. A description of behaviour you did not observe is worth nothing to the review. `TestResolvHijackerConcurrentResetRemove` fails for a non-root user; that one is known.

Visible text lives in `gui/src/locales/`: update all six locales together. `yarn i18n-check` compares key sets, not whether a translation followed a wording change. The in-app documentation under `gui/src/docs/` has the same six.

## Traps that cost a review round

- `go build ./...` in `service/` fails with `pattern web: no matching files found` while `service/server/router/web` is empty. `yarn --cwd gui build` writes to `web/` at the root; `./build.sh` and `./build-all.sh` copy it across.
- A Go build inside a container needs `-buildvcs=false` and `git config --global --add safe.directory /repo`. Without the first it stops at the VCS stamp when the checkout has no tags; without the second it refuses the mount. `build-all.sh` passes both — keep them if you run a build by hand.
- The containers write as root unless told otherwise: `gui/node_modules` and `web/` end up owned by root, and removing them then fails without `sudo`. `build-all.sh` runs the interface build as the invoking user and hands the binaries back with `chown`; if you run a container yourself, pass `--user "$(id -u):$(id -g)"`, or delete through a container as root.
- The locale files are `en, zh, fa-ir, ko, pt-br, ru`; the documentation directories are `en, zh, fa, ko, pt, ru`. The names differ. Both sets change together.
- A literal `@` in a locale string must be written `{'@'}`; vue-i18n reads a bare one as a linked message.
- A key that `i18n-check` cannot see statically belongs to a prefix listed in `gui/scripts/i18n-check.mjs`; add the prefix rather than the key.
- Colours come from the Material scheme: `rgb(var(--v-theme-<role>))`. Do not write a hex literal in a component; it cannot follow the theme.
- Type comes from the scale in `gui/src/theme/typography.scss` (`md3-title-large` and the rest), which uses weights 400 and 500; do not add 700.
- `core/` is a fork of xray-core. v2rayA's own code is `core/dns`, `core/hint` and `core/main`; leave the rest to upstream. After editing a `.proto` under `core/hint/proxy`, regenerate from the module root (`protoc --go_out=. --go_opt=paths=source_relative hint/proxy/<name>/config.proto`) so the descriptor keeps its path.
- Read a command's exit code; do not infer success from the end of its output. `eslint` prints its errors above the summary line.

## Code in this repository

Match the file you are editing: its comment density, its naming, its idiom. Comments state what is not evident from the code — a reason, a constraint, a protocol quirk — and stop there; no comment repeats what the next line says.

Reach for what is already there before adding anything: a dependency the module already has, a helper in `service/common` or `gui/src/lib`, a Vuetify component before a hand-written one. A new dependency needs a reason a reviewer would accept.

## What not to do

- Do not sign your work: no model `Co-Authored-By` trailer, no AI signature, no generated-by marker, in a comment, a commit message or pull request text. This is about the code and the commits; the README states in prose who wrote the changes and how they were decided, and that is where that belongs.
- Do not touch code the change does not require: no reformatting, no renaming, no regenerated lockfiles.
- Do not present a guess as a result. If you did not run it, say so.
- Do not open or comment on an issue or a pull request on your own, and do not fill in the field that names who answers for it. The human submits, and answers for what is submitted.

## Working in a fork

This checkout is a fork of [v2rayA](https://github.com/v2rayA/v2rayA). The product keeps the upstream name, layout and licence; the Go module path stays `github.com/v2rayA/v2rayA` and `github.com/v2rayA/v2raya-core`, because a module path is an address, not a label, and renaming 463 import sites buys a user nothing.

Every divergence from upstream is a cost paid at the next merge, so keep the diff as small as the change allows: no drive-by renames, no reformatting, no refactoring of code the change does not touch. Prefer a route that leaves the surrounding file recognisable to someone who knows upstream.

Where an existing third-party shape would do, use it and say so in the code. The list of what the project already stands on is in [docs/README.md](docs/README.md#what-the-code-stands-on); a solution that belongs to someone else stays theirs, with its licence, and this repository claims no credit for it.
