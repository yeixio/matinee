#!/usr/bin/env bash
# Builds .deb and .rpm packages from the Linux archives in dist/, and adds
# them to dist/SHA256SUMS.txt.
# Usage: VERSION=1.2.3 scripts/package-linux.sh  (run scripts/release-binaries.sh first)
# Needs nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.3
set -euo pipefail
: "${VERSION:?set VERSION, e.g. 1.2.3}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
command -v nfpm >/dev/null || { echo "nfpm is required: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.3" >&2; exit 1; }

# A pre-release such as 1.2.0-beta.1 is 1.2.0~beta.1, which apt and dnf sort
# before 1.2.0. sed, not ${VERSION/-/~}: bash tilde-expands that "~".
PKG_VERSION="$(printf '%s' "$VERSION" | sed 's/-/~/')"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# archive arch, deb arch, rpm arch (empty: no rpm)
targets=(
  "amd64 amd64 x86_64"
  "arm64 arm64 aarch64"
  # Fedora and RHEL dropped 32-bit ARM, so armv7 is deb only.
  "armv7 armhf -"
)
built=0
for t in "${targets[@]}"; do
  read -r arch debarch rpmarch <<<"$t"
  name="matinee_${VERSION}_linux_${arch}"
  if [ ! -f "dist/$name.tar.gz" ]; then
    echo "skipping linux/$arch: no dist/$name.tar.gz"
    continue
  fi
  tar -C "$work" -xzf "dist/$name.tar.gz"
  cfg="$work/$arch.yaml"
  cat > "$cfg" <<YAML
name: matinee
arch: ${debarch}
platform: linux
version: "${PKG_VERSION}"
maintainer: YEIXIO LLC <hello@yeix.io>
description: |-
  Self-hosted media server for movies, TV and music
  A fast, self-hosted media server for your movies, TV and music. Runs as
  matinee.service on port 7171, with its data in /var/lib/matinee.
homepage: https://github.com/yeixio/matinee
license: AGPL-3.0-or-later
section: video
contents:
  - src: ${work}/${name}/matinee
    dst: /usr/bin/matinee
    file_info:
      mode: 0755
  - src: ${ROOT}/packaging/linux/matinee.service
    dst: /usr/lib/systemd/system/matinee.service
  - src: ${ROOT}/packaging/linux/matinee.env
    dst: /etc/matinee/matinee.env
    type: config|noreplace
    file_info:
      mode: 0640
  - src: ${ROOT}/LICENSE
    dst: /usr/share/doc/matinee/LICENSE
  - src: ${ROOT}/README.md
    dst: /usr/share/doc/matinee/README.md
scripts:
  postinstall: ${ROOT}/packaging/linux/postinstall.sh
  preremove: ${ROOT}/packaging/linux/preremove.sh
overrides:
  deb:
    depends: [ffmpeg]
  rpm:
    # Fedora's ffmpeg-free and RPM Fusion's ffmpeg both provide these.
    depends: [/usr/bin/ffmpeg, /usr/bin/ffprobe, shadow-utils]
YAML
  if [ "$rpmarch" != - ]; then
    printf 'rpm:\n  arch: %s\n' "$rpmarch" >> "$cfg"
  fi
  nfpm package -p deb -f "$cfg" -t "dist/matinee_${VERSION}_${debarch}.deb"
  if [ "$rpmarch" != - ]; then
    nfpm package -p rpm -f "$cfg" -t "dist/matinee-${VERSION}-1.${rpmarch}.rpm"
  fi
  built=$((built + 1))
done
[ "$built" -gt 0 ] || { echo "no Linux archives in dist/" >&2; exit 1; }

(cd dist && shopt -s nullglob && sha256sum *.tar.gz *.zip *.deb *.rpm > SHA256SUMS.txt)
ls -l dist/*.deb dist/*.rpm
