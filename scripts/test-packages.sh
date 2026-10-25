#!/usr/bin/env bash
# Installs Matinee from the apt and dnf repositories the way the README says
# to, in fresh Debian, Ubuntu and Fedora containers, then starts it and
# checks /healthz. The repositories must be signed: apt and dnf check them
# against matinee.asc, never with trusted=yes or gpgcheck=0.
#
#   scripts/test-packages.sh          signs repositories built from dist/
#                                     with a key made for the test
#   scripts/test-packages.sh <dir>    tests repositories already built and
#                                     signed, such as the release's dist/packages
#
# DOCKER_TEST_ARGS adds docker run options, such as a proxy for the mirrors.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
server=""
cleanup() {
  [ -z "$server" ] || kill "$server" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

if [ $# -ge 1 ]; then
  repo="$(cd "$1" && pwd)"
else
  export GNUPGHOME="$work/gnupg"
  mkdir -m 700 "$GNUPGHOME"
  gpg --batch --quiet --pinentry-mode loopback --passphrase test \
    --quick-generate-key "Matinee package test <test@matinee.invalid>" rsa3072 sign 1d
  key="$(gpg --batch --pinentry-mode loopback --passphrase test --armor --export-secret-keys)"
  unset GNUPGHOME
  repo="$work/packages"
  PACKAGE_SIGNING_KEY="$key" PACKAGE_SIGNING_PASSPHRASE=test "$ROOT/scripts/package-repos.sh" "$repo"
fi
for f in matinee.asc apt/dists/stable/InRelease rpm/repodata/repomd.xml.asc; do
  [ -s "$repo/$f" ] || { echo "The repository has no $f: it isn't signed." >&2; exit 1; }
done

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
url="http://127.0.0.1:$port"
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$repo" >/dev/null 2>&1 &
server=$!
sleep 1

# Starts the installed server as the service would, and waits for /healthz.
check='
  test -f /etc/matinee/matinee.env
  getent passwd matinee >/dev/null
  runuser -u matinee -- env MATINEE_DATA_DIR=/var/lib/matinee MATINEE_ADDR=127.0.0.1:7199 /usr/bin/matinee >/tmp/matinee.log 2>&1 &
  for i in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:7199/healthz >/dev/null 2>&1; then echo "matinee is up"; exit 0; fi
    sleep 1
  done
  cat /tmp/matinee.log
  exit 1
'

for image in ${APT_TEST_IMAGES:-debian:12 ubuntu:24.04}; do
  echo "== apt on $image"
  docker run --rm --network host ${DOCKER_TEST_ARGS:-} "$image" bash -euo pipefail -c "
    apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates >/dev/null
    install -d -m 0755 /etc/apt/keyrings
    curl -fsSL $url/matinee.asc -o /etc/apt/keyrings/matinee.asc
    echo 'deb [signed-by=/etc/apt/keyrings/matinee.asc] $url/apt stable main' > /etc/apt/sources.list.d/matinee.list
    apt-get update 2>&1 | tee /tmp/update.log
    if grep -Eq '^(W|E|Err):' /tmp/update.log; then echo 'apt-get update warned or failed.' >&2; exit 1; fi
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq matinee >/dev/null
    $check
  "
done

for image in ${DNF_TEST_IMAGES:-fedora:latest}; do
  echo "== dnf on $image"
  docker run --rm --network host ${DOCKER_TEST_ARGS:-} "$image" bash -euo pipefail -c "
    curl -fsSL $url/matinee.repo | sed 's|https://[^ ]*/packages|$url|' > /etc/yum.repos.d/matinee.repo
    cat /etc/yum.repos.d/matinee.repo
    # gpgcheck=1: dnf refuses a package that isn't signed with matinee.asc.
    dnf install -y -q matinee util-linux
    $check
  "
done
echo "apt and dnf install Matinee from the signed repositories, and it starts."
