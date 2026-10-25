#!/bin/sh
# Writes Formula/matinee.rb from packaging/homebrew/matinee.rb.tmpl and the
# checksums in dist/SHA256SUMS.txt.
# Usage: VERSION=1.2.3 scripts/write-brew-formula.sh  (after scripts/release-binaries.sh)
set -eu
: "${VERSION:?set VERSION, e.g. 1.2.3}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
sums="$ROOT/dist/SHA256SUMS.txt"

sha() {
	s="$(awk -v f="matinee_${VERSION}_$1.tar.gz" '$2 == f {print $1}' "$sums")"
	[ -n "$s" ] || { echo "no checksum for matinee_${VERSION}_$1.tar.gz in $sums" >&2; exit 1; }
	echo "$s"
}
darwin_arm64="$(sha darwin_arm64)"
darwin_amd64="$(sha darwin_amd64)"
linux_arm64="$(sha linux_arm64)"
linux_amd64="$(sha linux_amd64)"

mkdir -p "$ROOT/Formula"
sed \
	-e "s/TAG/v${VERSION}/g" \
	-e "s/VERSION/${VERSION}/g" \
	-e "s/DARWIN_ARM64_SHA/${darwin_arm64}/" \
	-e "s/DARWIN_AMD64_SHA/${darwin_amd64}/" \
	-e "s/LINUX_ARM64_SHA/${linux_arm64}/" \
	-e "s/LINUX_AMD64_SHA/${linux_amd64}/" \
	"$ROOT/packaging/homebrew/matinee.rb.tmpl" > "$ROOT/Formula/matinee.rb"
echo "Wrote Formula/matinee.rb for $VERSION"
