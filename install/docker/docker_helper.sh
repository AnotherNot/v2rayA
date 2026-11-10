#!/bin/sh
# Build the Action image from a published release. The release carries a
# SHA-256 sidecar for every binary; a binary that does not match it is not
# installed. V2RAYA_VERSION is required and must be an exact release version.
set -eu

: "${V2RAYA_VERSION:?V2RAYA_VERSION is required}"
: "${V2RAYA_REPO:=AnotherNot/v2rayA}"

case "$(arch)" in
    x86_64) v2raya_arch="x64" ;;
    armv7l) v2raya_arch="armv7" ;;
    aarch64) v2raya_arch="arm64" ;;
    riscv64) v2raya_arch="riscv64" ;;
    *) echo "unsupported architecture: $(arch)" >&2; exit 1 ;;
esac

base="https://github.com/$V2RAYA_REPO/releases/download/v$V2RAYA_VERSION"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"

fetch_checksum() {
    # _release.yml writes <asset>.sha256.txt; release_binaries.yml writes
    # <asset>.sha256. Accept either because both are published assets.
    asset="$1"
    for suffix in .sha256.txt .sha256; do
        if wget -q -O "$asset$suffix" "$base/$asset$suffix" 2>/dev/null; then
            return 0
        fi
    done
    echo "no checksum published for $asset" >&2
    return 1
}

verify() {
    asset="$1"
    expected=$(awk '{print $1}' "$asset.sha256.txt" 2>/dev/null || awk '{print $1}' "$asset.sha256")
    actual=$(sha256sum "$asset" | awk '{print $1}')
    if [ "$expected" != "$actual" ]; then
        echo "checksum mismatch for $asset: expected $expected, got $actual" >&2
        exit 1
    fi
}

install_binary() {
    asset="$1"
    target="$2"
    wget -q -O "$asset" "$base/$asset"
    fetch_checksum "$asset"
    verify "$asset"
    install -m 0755 "$asset" "$target"
}

install_binary "v2raya_linux_${v2raya_arch}_$V2RAYA_VERSION" /usr/bin/v2raya
install_binary "v2raya_core_linux_${v2raya_arch}_$V2RAYA_VERSION" /usr/bin/v2raya_core

# These are rule databases, not executables; the core parses them on start.
# They are not covered by the release checksum.
mkdir -p /usr/share/v2raya
wget -q -O /usr/share/v2raya/LoyalsoldierSite.dat \
    https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geosite.dat
wget -q -O /usr/share/v2raya/geosite.dat \
    https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geosite.dat
wget -q -O /usr/share/v2raya/geoip.dat \
    https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geoip.dat

apk add --no-cache iptables iptables-legacy nftables tzdata
install -m 0755 /iptables.sh /usr/local/bin/iptables
install -m 0755 /ip6tables.sh /usr/local/bin/ip6tables
install -m 0755 /iptables.sh /usr/local/bin/iptables-nft
install -m 0755 /ip6tables.sh /usr/local/bin/ip6tables-nft
install -m 0755 /iptables.sh /usr/local/bin/iptables-legacy
install -m 0755 /ip6tables.sh /usr/local/bin/ip6tables-legacy
