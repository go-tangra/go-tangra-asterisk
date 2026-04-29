#!/bin/sh
set -e

if systemctl is-active --quiet tangra-asterisk 2>/dev/null; then
    systemctl stop tangra-asterisk
fi

if systemctl is-enabled --quiet tangra-asterisk 2>/dev/null; then
    systemctl disable tangra-asterisk
fi
