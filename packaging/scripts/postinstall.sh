#!/bin/sh
# deb: $1=configure, $2=previous version on upgrade. rpm: $1=1 install, 2 upgrade.
set -e
upgrade=0
case "$1" in
  configure) [ -n "$2" ] && upgrade=1 ;;
  2) upgrade=1 ;;
esac
chown root:tangra-asterisk /etc/tangra-asterisk
chmod 0750 /etc/tangra-asterisk
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
  if [ "$upgrade" = 1 ]; then
    systemctl try-restart tangra-asterisk.service || true
  else
    echo "tangra-asterisk: configure it from the join bundle: bash install-standalone.sh (in the bundle directory)"
  fi
fi
exit 0
