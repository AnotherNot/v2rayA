# Releasing v2rayB

Releases are cut from tags through the **Build & Release v2rayA**
workflow (Actions → Run workflow → `tag`, e.g. `v2.2.7.2`). Everything
below runs unattended once the one-time setup is done.

## One-time setup (owner actions in GitHub)

1. **Container registry.** Nothing to do: `ghcr.io` images publish with
   the built-in `GITHUB_TOKEN`. After the first push, open the package
   page and switch its visibility if it should be public.
2. **Release automation token.** Create a classic personal access token
   with the `public_repo` scope and store it as the `RELEASE_PAT`
   repository secret. It submits the winget manifest and pushes the
   scoop bucket.
3. **Scoop bucket.** Create an empty public repository
   `AnotherNot/scoop-v2rayb` (a README is enough). The release workflow
   writes `bucket/v2rayb.json` into it. Users then run:
   `scoop bucket add v2rayb https://github.com/AnotherNot/scoop-v2rayb`
   and `scoop install v2rayb`.
4. **First winget release.** The workflow tries `wingetcreate update`
   and falls back to `wingetcreate new` when the package does not exist
   yet, so the first release bootstraps itself. Watch that first run:
   if the fallback flags do not satisfy a newer wingetcreate, complete
   the submission once by hand and later releases go through `update`.

## What one release publishes

- GitHub release with the binaries, the Windows installers, the Linux
  packages, `web.tar.gz`/`web.zip` and their `.sha256.txt` files.
- `ghcr.io/<owner>/v2rayb:<tag>` and `:latest`, plus `v2rayb-gui:latest`,
  built from source (`install/docker/Dockerfile`); `amd64` and `arm64`.
- A winget version bump for `AnotherNot.v2rayB`.
- A scoop manifest bump in `AnotherNot/scoop-v2rayb`.

Pre-releases (`beta_release_main.yml`) publish the GitHub release only;
no downstream channel is touched.

## Deliberately not published

Homebrew, AUR, COPR, Docker Hub, the snap store and third-party APT
repositories still serve the upstream original. The README marks those
channels as such; moving any of them is a separate task per channel.
