#!/bin/sh
# deb: $1=install|upgrade. rpm: $1=1 install, 2 upgrade.
set -e
if ! getent group tangra-asterisk >/dev/null 2>&1; then
  groupadd --system tangra-asterisk
fi
if ! getent passwd tangra-asterisk >/dev/null 2>&1; then
  useradd --system --gid tangra-asterisk --home-dir /var/lib/tangra-asterisk \
    --no-create-home --shell /usr/sbin/nologin --comment "go-tangra asterisk module" tangra-asterisk
fi
exit 0
