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
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
  systemctl enable matinee.service || true
  systemctl restart matinee.service || true
  echo "Matinee is running. Open http://$(hostname):7171 to set it up."
fi
