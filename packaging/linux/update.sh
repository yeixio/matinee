#!/bin/sh
# Upgrades the matinee package when an admin asks in Settings → Updates
# (matinee-update.path runs it, as root). It only ever upgrades matinee,
# from the repositories this system already uses, and the package's own
# install script restarts the server. It writes only its log, never into
# the server's data directory, which the matinee user owns.
set -u
rm -f /var/lib/matinee/update/request
log=/var/log/matinee-update.log
umask 022
: > "$log"
exec >> "$log" 2>&1
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) upgrading matinee"
before=$( (dpkg-query -W -f '${Version}' matinee || rpm -q --qf '%{VERSION}' matinee) 2>/dev/null)
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -q &&
    apt-get install -y -q --only-upgrade -o Dpkg::Options::=--force-confold matinee
elif command -v dnf >/dev/null 2>&1; then
  dnf -y --refresh upgrade matinee
elif command -v yum >/dev/null 2>&1; then
  yum -y upgrade matinee
else
  echo "no apt-get, dnf or yum here"
  false
fi
status=$?
after=$( (dpkg-query -W -f '${Version}' matinee || rpm -q --qf '%{VERSION}' matinee) 2>/dev/null)
if [ "$status" -ne 0 ]; then
  echo "result: failed"
elif [ "$before" = "$after" ]; then
  echo "result: unchanged"
else
  echo "upgraded $before to $after"
  echo "result: ok"
fi
