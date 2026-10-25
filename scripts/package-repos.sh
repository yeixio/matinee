#!/usr/bin/env bash
# Builds the apt and dnf/yum repositories from the packages in dist/, into a
# directory the release publishes as the packages branch:
#
#   matinee.asc    public signing key
#   matinee.repo   dnf/yum repository file
#   apt/           deb [signed-by=...] <url>/apt stable main
#   rpm/           baseurl=<url>/rpm
#
# Usage: scripts/package-repos.sh [out-dir]  (default dist/packages)
#
# PACKAGE_SIGNING_KEY (an armored secret key) and PACKAGE_SIGNING_PASSPHRASE
# sign the apt index (InRelease, Release.gpg), each rpm, and the rpm index
# (repomd.xml.asc). PACKAGE_REQUIRE_SIGNING=1 makes a missing key an error,
# as a stable release does, so an unsigned repository is never published.
# PACKAGES_URL is where the directory is served (the packages branch).
# Needs dpkg-scanpackages (dpkg-dev), apt-ftparchive (apt-utils),
# createrepo_c (createrepo-c), rpmsign (rpm) and gpg.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
OUT="${1:-dist/packages}"
DEBS="${PACKAGE_DEBS:-dist}"
URL="${PACKAGES_URL:-https://raw.githubusercontent.com/yeixio/matinee/packages}"
for tool in dpkg-scanpackages apt-ftparchive createrepo_c rpmsign gpg; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

rm -rf "$OUT"
mkdir -p "$OUT/apt/pool/main" "$OUT/rpm"
cp "$DEBS"/*.deb "$OUT/apt/pool/main/"
shopt -s nullglob
rpms=("$DEBS"/*.rpm)
shopt -u nullglob
[ ${#rpms[@]} -eq 0 ] || cp "${rpms[@]}" "$OUT/rpm/"

signed=""
if [ -n "${PACKAGE_SIGNING_KEY:-}" ]; then
  GNUPGHOME="$(mktemp -d)"
  export GNUPGHOME
  chmod 700 "$GNUPGHOME"
  trap 'rm -rf "$GNUPGHOME"' EXIT
  printf '%s\n' "$PACKAGE_SIGNING_KEY" | gpg --batch --quiet --import
  fpr="$(gpg --batch --with-colons --list-secret-keys | awk -F: '/^fpr:/ {print $10; exit}')"
  [ -n "$fpr" ] || { echo "PACKAGE_SIGNING_KEY has no secret key in it." >&2; exit 1; }
  printf '%s' "${PACKAGE_SIGNING_PASSPHRASE:-}" > "$GNUPGHOME/passphrase"
  sign() {
    gpg --batch --yes --pinentry-mode loopback --passphrase-file "$GNUPGHOME/passphrase" \
      --local-user "$fpr" --digest-algo SHA512 "$@"
  }
  gpg --batch --armor --export "$fpr" > "$OUT/matinee.asc"
  signed=1
elif [ "${PACKAGE_REQUIRE_SIGNING:-}" = 1 ]; then
  echo "PACKAGE_SIGNING_KEY isn't set, and this repository must be signed." >&2
  exit 1
else
  echo "Not signed: PACKAGE_SIGNING_KEY isn't set." >&2
fi

# apt
archs="amd64 arm64 armhf"
for arch in $archs; do
  dest="dists/stable/main/binary-$arch"
  mkdir -p "$OUT/apt/$dest"
  (
    cd "$OUT/apt"
    dpkg-scanpackages -a "$arch" pool/main /dev/null > "$dest/Packages" 2>/dev/null
    gzip -9c "$dest/Packages" > "$dest/Packages.gz"
  )
done
cat > "$OUT/apt/dists/stable/Release" <<RELEASE
Origin: Matinee
Label: Matinee
Suite: stable
Codename: stable
Architectures: $archs
Components: main
Description: Matinee media server
RELEASE
apt-ftparchive release "$OUT/apt/dists/stable" >> "$OUT/apt/dists/stable/Release"
if [ -n "$signed" ]; then
  sign --clearsign -o "$OUT/apt/dists/stable/InRelease" "$OUT/apt/dists/stable/Release"
  sign --armor --detach-sign -o "$OUT/apt/dists/stable/Release.gpg" "$OUT/apt/dists/stable/Release"
  gpg --batch --quiet --verify "$OUT/apt/dists/stable/InRelease"
fi

# rpm: sign each package, then the index over them.
if [ -n "$signed" ] && [ ${#rpms[@]} -gt 0 ]; then
  rpmsign --addsign \
    --define "_gpg_name $fpr" \
    --define "_gpg_path $GNUPGHOME" \
    --define "_gpg_digest_algo sha512" \
    --define "_gpg_sign_cmd_extra_args --batch --pinentry-mode loopback --passphrase-file $GNUPGHOME/passphrase" \
    "$OUT"/rpm/*.rpm >/dev/null
fi
createrepo_c --quiet "$OUT/rpm"
if [ -n "$signed" ]; then
  sign --armor --detach-sign -o "$OUT/rpm/repodata/repomd.xml.asc" "$OUT/rpm/repodata/repomd.xml"
fi
check=0
[ -n "$signed" ] && check=1
cat > "$OUT/matinee.repo" <<REPO
[matinee]
name=Matinee
baseurl=$URL/rpm
enabled=1
gpgcheck=$check
repo_gpgcheck=$check
gpgkey=$URL/matinee.asc
REPO
echo "Package repositories: $OUT${signed:+ (signed with $fpr)}"
