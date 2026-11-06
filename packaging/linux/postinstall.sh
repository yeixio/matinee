#!/bin/sh
set -e
if ! getent passwd matinee >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/matinee --shell /usr/sbin/nologin matinee
fi
# Hardware transcoding opens the GPU's render device (/dev/dri/renderD*),
# which the render group (video on older systems) may use (#8). Only groups
# this system has.
for group in render video; do
  if getent group "$group" >/dev/null 2>&1; then
    usermod -a -G "$group" matinee || true
  fi
done
install -d -m 0750 -o matinee -g matinee /var/lib/matinee
# Where the server asks matinee-update.path to install an update.
install -d -m 0750 -o matinee -g matinee /var/lib/matinee/update
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
  systemctl enable matinee.service || true
  # Lets admins install updates from Settings → Updates. Only on a first
  # install (deb: configure with no old version, rpm: 1), so an admin who
  # turned it off keeps it off through upgrades.
  if { [ "$1" = configure ] && [ -z "$2" ]; } || [ "$1" = 1 ]; then
    systemctl enable matinee-update.path || true
  fi
  if systemctl is-enabled --quiet matinee-update.path 2>/dev/null; then
    systemctl start matinee-update.path || true
  fi
  systemctl restart matinee.service || true
  echo "Matinee is running. Open http://$(hostname):7171 to set it up."
fi
