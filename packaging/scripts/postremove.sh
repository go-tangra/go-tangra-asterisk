#!/bin/sh
# deb: $1=remove|purge|upgrade. rpm: $1=0 remove, 1 upgrade.
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
fi
# A purge also forgets the configuration, the enrolment token and the module's
# mesh identity (the PBX then needs a new join bundle).
if [ "$1" = purge ]; then
  rm -rf /etc/tangra-asterisk /var/lib/tangra-asterisk
fi
exit 0
