#!/usr/bin/env bash
#
# build-all.sh — one-command build of v2raya + v2raya_core from this checkout.
#
# Everything heavy runs in containers; the host only needs git and Docker.
# Outputs land in the repository root: ./v2raya and ./v2raya_core.
#
# Usage: ./build-all.sh [version]
#   version defaults to `git describe --tags --always --dirty`.
#
# What the build needs (all of it lives in containers, the host stays clean):
#   - git: only to derive the version string (override it with the argument).
#   - Docker Engine with a running daemon: image pulls are node:24 for the
#     web interface and golang:1.26 for the Go binaries.
#   - Network access to the Docker registry and the Go module proxy.
#   - About 2 GiB free for the build containers and the module cache volume.
#
# The Go module cache persists in the `v2raya-build-cache` Docker volume so
# repeated builds do not re-download the world; drop it any time with
# `docker volume rm v2raya-build-cache`.

set -euo pipefail

STEP="init"
trap 'echo "build-all.sh: step \"$STEP\" failed (exit $?)" >&2; exit 1' ERR

root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
version="${1:-$(git -C "$root_dir" describe --tags --always --dirty)}"
web_build_dir="$root_dir/.web-build"
web_target_dir="$root_dir/service/server/router/web"
node_image="node:24"
go_image="golang:1.26"
uid_gid="$(id -u):$(id -g)"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "build-all.sh: missing required tool: $1" >&2
    echo "build-all.sh: install $2" >&2
    exit 1
  fi
}

docker_run() {
  docker run --rm "$@"
}

STEP="check dependencies"
need git "git from your distribution (only used for the default version string)"
need docker "Docker Engine, https://docs.docker.com/engine/install/ ; then start the daemon"
if ! docker info >/dev/null 2>&1; then
  echo "build-all.sh: the Docker daemon is not reachable" >&2
  echo "build-all.sh: start it (usually: sudo systemctl start docker) and retry" >&2
  exit 1
fi

STEP="build the web interface (node:24)"
docker_run \
  --user "$uid_gid" \
  -v "$root_dir:/repo" -w /repo/gui \
  -e OUTPUT_DIR=/repo/.web-build \
  "$node_image" \
  bash -c 'corepack enable >/dev/null 2>&1; yarn install --frozen-lockfile && yarn build'

STEP="stage the web files for the service"
if [ ! -f "$web_build_dir/index.html" ]; then
  echo "build-all.sh: the web build produced no index.html in $web_build_dir" >&2
  exit 1
fi
rm -rf "$web_target_dir"
mkdir -p "$web_target_dir"
cp -a "$web_build_dir/." "$web_target_dir/"
rm -rf "$web_build_dir"

STEP="build v2raya_core (golang:1.26)"
docker_run \
  -v "$root_dir:/repo" -w /repo/core \
  -v v2raya-build-cache:/go/pkg/mod \
  "$go_image" \
  bash -c "git config --global --add safe.directory /repo && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /repo/v2raya_core -ldflags='-X main.Version=$version -s -w' ./main"

STEP="build v2raya (golang:1.26)"
docker_run \
  -v "$root_dir:/repo" -w /repo/service \
  -v v2raya-build-cache:/go/pkg/mod \
  "$go_image" \
  bash -c "git config --global --add safe.directory /repo && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /repo/v2raya -ldflags='-X github.com/v2rayA/v2rayA/conf.Version=$version -s -w' ."

STEP="smoke-check the outputs"
for bin in "$root_dir/v2raya" "$root_dir/v2raya_core"; do
  if [ ! -x "$bin" ]; then
    echo "build-all.sh: expected an executable at $bin" >&2
    exit 1
  fi
done
# The Go build runs as root inside the container; hand the binaries back to
# the invoking user so no sudo is needed to run or replace them.
docker_run -v "$root_dir:/repo" alpine chown "$uid_gid" /repo/v2raya /repo/v2raya_core
ls -la "$root_dir/v2raya" "$root_dir/v2raya_core"
echo "build-all.sh: OK ($version)"
