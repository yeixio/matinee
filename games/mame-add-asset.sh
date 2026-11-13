#!/bin/sh
# mame-add-asset.sh TAG FILE: adds FILE to the MAME release TAG and its
# checksum to the release's SHA256SUMS, once the release exists. The Mac
# and Windows builds each add theirs this way, so they're checked again
# afterwards: two adding at once could each drop the other's line.
set -eu
tag=$1 file=$2 repo=${GITHUB_REPOSITORY:-yeixio/matinee}
name=$(basename "$file")
sum() { { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } | cut -d' ' -f1; }
want="$(sum "$file")  $name"
for _ in $(seq 60); do gh release view "$tag" --repo "$repo" >/dev/null 2>&1 && break; sleep 60; done
dir=$(mktemp -d)
for try in 1 2 3 4 5; do
	gh release download "$tag" --repo "$repo" --pattern SHA256SUMS --dir "$dir" --clobber
	grep -v "  $name\$" "$dir/SHA256SUMS" >"$dir/new" || true
	echo "$want" >>"$dir/new"
	mv "$dir/new" "$dir/SHA256SUMS"
	gh release upload "$tag" "$file" "$dir/SHA256SUMS" --clobber --repo "$repo"
	sleep $((try * 7))
	gh release download "$tag" --repo "$repo" --pattern SHA256SUMS --dir "$dir" --clobber
	if grep -qx "$want" "$dir/SHA256SUMS"; then
		cat "$dir/SHA256SUMS"
		exit 0
	fi
done
echo "couldn't add $name to $tag's SHA256SUMS" >&2
exit 1
