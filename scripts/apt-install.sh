#!/usr/bin/env bash
# Installs Ubuntu packages on a CI runner, trying again for a few minutes
# when the runner can't reach Ubuntu's mirrors (its DNS sometimes can't
# resolve archive.ubuntu.com or security.ubuntu.com for a while).
# Usage: scripts/apt-install.sh ffmpeg nsis ...
set -euo pipefail
for try in 1 2 3 4 5; do
  if sudo apt-get -o Acquire::Retries=3 update &&
    sudo apt-get -o Acquire::Retries=3 install -y --no-install-recommends "$@"; then
    exit 0
  fi
  [ "$try" = 5 ] && break
  echo "apt-get failed (try $try of 5); trying again in $((try * 30))s" >&2
  sleep $((try * 30))
done
exit 1
