# Contributing to v2rayA

Usage questions go to [Discussions](https://github.com/AnotherNot/v2rayA/discussions). Issues are for a defect or a feature request, pull requests for a change you have built and run. The issue forms and the pull request template come in English, Russian and Chinese.

Before you open an issue or a pull request, check the [latest release](https://github.com/AnotherNot/v2rayA/releases/latest): fixes go into the current release only. Then search the existing issues.

## The repository

| Where | What |
|---|---|
| `service/` | the Go service: API, settings database, transparent proxy, the generated core config |
| `core/` | `v2raya_core`, a fork of xray-core with the DNS module, the in-core TUN and the extra protocols |
| `gui/` | the web interface: Vue 3, Vuetify 4 (Material 3), vue-i18n, Vitest |
| `install/` | packaging: systemd and OpenRC units, AUR, Docker, the Windows installer |

`./build.sh`, from the repository root, builds the web interface and both binaries; it needs Go 1.27 and Node 24 with yarn. The service embeds `service/server/router/web`, which only `build.sh` fills: `yarn --cwd gui build` writes to `web/` at the root, and `go build` in `service/` fails until those files are copied across.

## Before a pull request

Run the checks CI runs, from the repository root:

```sh
(cd gui     && yarn lint && yarn typecheck && yarn i18n-check && yarn test && yarn build)
(cd service && go build ./... && go vet ./... && go test ./...)
(cd core    && go build ./... && go vet ./...)
```

Then exercise the change in the running program: a service change by starting `v2raya` and using the feature, a GUI change in a browser, at phone width and in both themes. `TestResolvHijackerConcurrentResetRemove` fails when the tests run as a non-root user; that one is known.

The browser tests drive the built service rather than a mock: `./build-all.sh`, then `yarn --cwd gui playwright install chromium` once, then `yarn --cwd gui test:e2e`. The run starts `./v2raya` on `127.0.0.1:21117` with a scratch configuration directory, registers the first account and imports a node with both VLESS encryption and the REALITY ML-DSA-65 verify key.

- One change per pull request, and each commit a coherent step of it: no mixed-up commits, no reformatting of untouched code, no lockfile churn.
- Visible text lives in `gui/src/locales/`: update all six locales together. `yarn i18n-check` compares key sets, not whether a translation followed a wording change. The in-app documentation under `gui/src/docs/` has the same six.
- Do not add a model `Co-Authored-By` trailer, an AI signature or a generated-by marker to a commit, pull request text or a comment.
- One topic per commit, imperative subject saying what changes, `fix:` / `feat:` / `docs:` / `ci:` as the history uses.

## Releasing

Releases come from the **Release Binaries & Images** workflow, started by hand in Actions: choose `public`, enter the tag to create (`v2.5.14`), tick the box if it is a pre-release, and leave `dry_run` off. It needs nothing prepared beforehand and uses only the token Actions provides.

The run builds the interface, then `v2raya` and `v2raya_core` for Linux and Windows on amd64 and arm64. Each pair is packed with `LICENSE` into a `.tar.gz` or `.zip`; the Linux archives become `deb`, `rpm`, `apk` and Arch packages, and the Windows ones become Inno Setup installers. The release carries those four archives, the eight Linux packages, the two installers and one `SHA256SUMS`; the bare binaries are an intermediate and are never published. The image goes to `ghcr.io/anothernot/v2raya` under the tag, the bare version and `latest`. The tag is created at the commit the run was started from, and the release waits for the packages, the installers and the image; a tag that already points at another commit stops the run instead.

`dry_run` builds and checks everything without creating the tag, the release or an image push, and the intermediate artifacts expire on their own. Use it to validate a change to the workflow.

The packages and installers carry pinned copies of `geoip.dat` and `geosite.dat`. The service updates its own copy at runtime and verifies the download against the checksum published next to it, so a pinned release is not a frozen one.

Publishing to winget and scoop needs the repository secret `RELEASE_PAT`; the separate **Release to AUR** workflow needs `AUR_SSH_PRIVATE_KEY`. The winget and scoop jobs are skipped for a pre-release, and a job whose secret is missing fails after the release is published rather than silently skipping its step.

## AI-assisted contributions

An assistant may write and test the code, and you verify it before you submit it. Point it at [AGENTS.md](AGENTS.md), which states what this repository expects of it.

- Read its output before you push. If you cannot say why a line is there, it does not go in.
- Execute what it wrote. Do not present an untested fix as verified.
- Watch for the wrong root cause, argued well. Ask what else could produce the symptom.
- Keep the diff to the change; do not let the assistant tidy unrelated code or regenerate lockfiles.
- Between review rounds, be able to say in one sentence what changed and why.
- Say which part it wrote. That is not held against you; hiding it is.

Maintainers close an issue or a pull request submitted without human verification, or with a checklist checked without reading. Each checklist carries two statements that are false for an honest submitter; checking either one is the admission.
