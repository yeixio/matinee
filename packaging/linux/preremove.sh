#!/bin/sh
set -e
# Only when the package is removed: deb passes "remove", rpm passes 0. On an
# upgrade the new package's scripts restart the service. The data in
# /var/lib/matinee stays.
case "$1" in
remove | 0)
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl disable --now matinee.service || true
  fi
  ;;
esac
