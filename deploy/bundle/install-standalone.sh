#!/usr/bin/env bash
# Install asterisk as a standalone systemd service instead of the container:
# downloads this release's tangra-asterisk .deb or .rpm from GitHub, checks it
# against the checksums pinned in this bundle, installs it and writes
# /etc/tangra-asterisk from the configuration the gateway generated here
# (config.yaml, policy.yaml, .env, private/).
#
#   sudo bash install-standalone.sh [--package FILE] [--latest] [--adopt-registration] [--reconfigure] [--no-start]
#
#   --package FILE  install this .deb/.rpm instead of downloading it (it must
#                   still match the checksums pinned in this bundle)
#   --latest        install the latest release instead of this bundle's; it is
#                   checked against that release's SHA256SUMS only
#   --adopt-registration  the registration database holds a v3 history
#                   (pjsip_registration_events): keep and continue it
#   --reconfigure   rewrite /etc/tangra-asterisk/config.yaml from this bundle
#                   even though it exists (the old file is kept as a .bak)
#   --no-start      configure everything, but do not enable or start the service
#
# Run it again to upgrade: an existing /etc/tangra-asterisk/config.yaml (with
# any edits made on this host) is kept; --reconfigure re-applies the bundle's.
# Remove with `apt purge
# tangra-asterisk` / `dnf remove tangra-asterisk` (purge also forgets the
# module's mesh identity).
set -euo pipefail

REPO=go-tangra/go-tangra-asterisk
NAME=tangra-asterisk
ETC=/etc/tangra-asterisk
STATE=/var/lib/tangra-asterisk
UNIT=tangra-asterisk.service

die() { echo "install-standalone: $*" >&2; exit 1; }
note() { echo "install-standalone: $*"; }

package="" latest=0 start=1 adopt=0 reconfigure=0
while [ $# -gt 0 ]; do
  case "$1" in
    --package) [ $# -ge 2 ] || die "--package needs a file"; package="$(realpath "$2")"; shift 2 ;;
    --latest) latest=1; shift ;;
    --no-start) start=0; shift ;;
    --adopt-registration) adopt=1; shift ;;
    --reconfigure) reconfigure=1; shift ;;
    -h|--help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
done

[ "$(id -u)" = 0 ] || die "run as root (sudo bash $0)"
[ -d /run/systemd/system ] || die "systemd is required"
cd "$(dirname "$(realpath "$0")")"
for f in .env config.yaml policy.yaml private/enrollment.token; do
  [ -f "$f" ] || die "$f is missing: run this from the asterisk join bundle directory"
done

# .env: KEY="value" lines written by the gateway (values never contain quotes).
declare -A dotenv
while IFS= read -r line; do
  if [[ "$line" =~ ^([A-Z][A-Z0-9_]*)=\"(.*)\"$ ]]; then dotenv["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"; fi
done < .env
for k in MODULE_VERSION MODULE_BIND_IP ASTERISK_RECORDINGS_DIR; do
  [ -n "${dotenv[$k]:-}" ] || die ".env has no $k"
done
bind_ip="${dotenv[MODULE_BIND_IP]}"
recordings="${dotenv[ASTERISK_RECORDINGS_DIR]}"
[[ "$bind_ip" =~ ^[0-9.]+$ ]] || die "MODULE_BIND_IP is not an IPv4 address: $bind_ip"
[[ "$recordings" =~ ^/[A-Za-z0-9._/-]*$ ]] || die "ASTERISK_RECORDINGS_DIR is not a plain absolute path: $recordings"

# The container and the service would share the module's identity and ports.
if command -v docker >/dev/null 2>&1 &&
  [ -n "$(docker ps -q --filter label=com.docker.compose.project=tangra-asterisk 2>/dev/null)" ]; then
  die "the asterisk container is running: stop it first (docker compose down, in this directory)"
fi

# Package format and architecture.
case "$(uname -m)" in
  x86_64|amd64) deb_arch=amd64 rpm_arch=x86_64 ;;
  aarch64|arm64) deb_arch=arm64 rpm_arch=aarch64 ;;
  *) die "unsupported architecture $(uname -m) (amd64 and arm64 only)" ;;
esac
if command -v apt-get >/dev/null 2>&1; then
  fmt=deb
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1 || command -v zypper >/dev/null 2>&1; then
  fmt=rpm
else
  die "no apt-get, dnf, yum or zypper found"
fi

version="${dotenv[MODULE_VERSION]#v}"
if [ "$latest" = 1 ]; then
  tag="$(curl -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/^ *"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "could not find the latest release of $REPO"
  version="${tag#v}"
  [ "$version" = "${dotenv[MODULE_VERSION]#v}" ] ||
    note "installing $version; this configuration was generated for ${dotenv[MODULE_VERSION]#v}"
fi
if [ "$fmt" = deb ]; then file="${NAME}_${version}_${deb_arch}.deb"; else file="${NAME}-${version}-1.${rpm_arch}.rpm"; fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fetch() { curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$2" "https://github.com/$REPO/releases/download/v$version/$1"; }

# Checksums: pinned in this bundle (packages.sha256, part of the attested
# bundle) for the bundle's own release; the release's SHA256SUMS otherwise.
if [ "$version" = "${dotenv[MODULE_VERSION]#v}" ] && [ -f packages.sha256 ]; then
  sums=packages.sha256
else
  [ -f packages.sha256 ] || note "this bundle pins no package checksums; using the release's SHA256SUMS"
  fetch SHA256SUMS "$work/SHA256SUMS" || die "could not download SHA256SUMS of v$version"
  sums="$work/SHA256SUMS"
fi
want="$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$sums")"
[ -n "$want" ] || die "$file is not listed in $sums"

if [ -z "$package" ]; then
  note "downloading $file (v$version)"
  fetch "$file" "$work/$file" || die "could not download $file from the v$version release"
  package="$work/$file"
fi
[ -f "$package" ] || die "$package does not exist"
got="$(sha256sum "$package" | awk '{print $1}')"
[ "$got" = "$want" ] || die "checksum mismatch for $(basename "$package"): got $got, want $want"
note "checksum ok ($sums)"

case "$fmt" in
  deb) DEBIAN_FRONTEND=noninteractive apt-get install -y "$package" ;;
  rpm)
    if command -v dnf >/dev/null 2>&1; then dnf install -y "$package"
    elif command -v yum >/dev/null 2>&1; then yum install -y "$package"
    else zypper --non-interactive install --allow-unsigned-rpm "$package"; fi ;;
esac
getent passwd "$NAME" >/dev/null || die "the package did not create the $NAME user"

install -d -m 0750 -o root -g "$NAME" "$ETC" "$ETC/private"
backup() { cp -p "$ETC/config.yaml" "$ETC/config.yaml.bak-$(date -u +%Y%m%d-%H%M%S)"; }
if [ -f "$ETC/config.yaml" ] && [ "$reconfigure" = 0 ]; then
  # An upgrade keeps the configuration in use: it may hold edits made on
  # this host (DSNs, AMI credentials) that the bundle does not have.
  note "keeping $ETC/config.yaml (run with --reconfigure to rewrite it from this bundle)"
  if [ "$adopt" = 1 ] && ! grep -q '^  adopt_registration:' "$ETC/config.yaml"; then
    grep -q '^  registration_dsn: ' "$ETC/config.yaml" || die "$ETC/config.yaml has no registration_dsn to adopt"
    backup
    sed -i 's/^\(  registration_dsn: .*\)$/\1\n  adopt_registration: true/' "$ETC/config.yaml"
  fi
else
  # The bundle's config.yaml is written for the container's paths: point it at
  # this host's (state, token, policy, recordings) and listen on the mesh IP
  # only, as the compose file's port bindings did.
  sed -e "s#token_file: /run/secrets/asterisk.token#token_file: $ETC/private/enrollment.token#" \
      -e "s#state_file: /var/lib/asterisk-module/svid.json#state_file: $STATE/svid.json#" \
      -e "s#path: /etc/asterisk-module/policy.yaml#path: $ETC/policy.yaml#" \
      -e "s#recording_root: /recordings\$#recording_root: $recordings#" \
      -e "s#http_addr: 0.0.0.0:8444#http_addr: $bind_ip:8444#" \
      -e "s#grpc_addr: 0.0.0.0:9444#grpc_addr: $bind_ip:9444#" \
      config.yaml > "$work/config.yaml"
  if [ "$adopt" = 1 ] && ! grep -q '^  adopt_registration:' "$work/config.yaml"; then
    sed -i 's/^\(  registration_dsn: .*\)$/\1\n  adopt_registration: true/' "$work/config.yaml"
  fi
  for want_line in "token_file: $ETC/private/enrollment.token" "state_file: $STATE/svid.json" \
    "path: $ETC/policy.yaml" "recording_root: $recordings" "http_addr: $bind_ip:8444" "grpc_addr: $bind_ip:9444"; do
    grep -qF "$want_line" "$work/config.yaml" || die "config.yaml has an unexpected layout: could not set '$want_line'"
  done
  if [ -f "$ETC/config.yaml" ] && ! cmp -s "$work/config.yaml" "$ETC/config.yaml"; then backup; fi
  install -m 0640 -o root -g "$NAME" "$work/config.yaml" "$ETC/config.yaml"
fi
install -m 0640 -o root -g "$NAME" policy.yaml "$ETC/policy.yaml"
# The service reads the token on every start; it is used only to enrol.
install -m 0640 -o root -g "$NAME" private/enrollment.token "$ETC/private/enrollment.token"
if [ -f private/mesh-ca.pem ]; then install -m 0644 -o root -g root private/mesh-ca.pem "$ETC/mesh-ca.pem"; fi
install -d -m 0700 -o "$NAME" -g "$NAME" "$STATE"

# A container that already enrolled left the module's identity in its
# volume: keep it, so the (single-use) token is not needed again.
if [ ! -f "$STATE/svid.json" ] && command -v docker >/dev/null 2>&1; then
  vol="$(docker volume inspect -f '{{.Mountpoint}}' tangra-asterisk_asterisk-state 2>/dev/null || true)"
  if [ -n "$vol" ] && [ -f "$vol/svid.json" ]; then
    install -m 0600 -o "$NAME" -g "$NAME" "$vol/svid.json" "$STATE/svid.json"
    note "kept the mesh identity the container enrolled"
  fi
fi

# Recordings are usually readable by the PBX's group only (e.g. asterisk).
group=""
dropin=/etc/systemd/system/$UNIT.d
rm -f "$dropin/recordings.conf"
if [ -d "$recordings" ]; then
  group="$(stat -c %G "$recordings")"
  if [ "$group" != root ] && [ "$group" != UNKNOWN ]; then
    install -d -m 0755 "$dropin"
    printf '# Written by install-standalone.sh: read %s.\n[Service]\nSupplementaryGroups=%s\n' "$recordings" "$group" > "$dropin/recordings.conf"
    note "the service reads $recordings as group $group"
  fi
else
  note "warning: $recordings does not exist; recordings stay off until it does"
fi
systemctl daemon-reload

note "preflight"
if ! runuser -u "$NAME" -g "$NAME" ${group:+-G "$group"} -- /usr/bin/asterisksvc preflight -config "$ETC/config.yaml"; then
  die "preflight failed: fix the problems above and run this script again"
fi

# Registration history needs its module-owned tables (idempotent).
if grep -qE '^  registration_dsn: "[^"]+"' "$ETC/config.yaml"; then
  note "registration database: bootstrap"
  if ! out="$(runuser -u "$NAME" -g "$NAME" -- /usr/bin/asterisksvc bootstrap -config "$ETC/config.yaml" 2>&1)"; then
    echo "$out" >&2
    case "$out" in
      *adopt_registration*) die "the registration database holds a v3 history: run again with --adopt-registration to keep it, or give an empty database" ;;
      *) die "registration bootstrap failed (see above); the rest is configured" ;;
    esac
  fi
fi

if [ "$start" = 1 ]; then
  systemctl enable "$UNIT" >/dev/null
  systemctl restart "$UNIT"
  sleep 3
  systemctl is-active --quiet "$UNIT" || die "$UNIT did not stay up: journalctl -u $UNIT -n 50"
  note "$UNIT is running; it enrols and registers with the gateway on its own (journalctl -u $UNIT -f)"
else
  note "configured; start it with: systemctl enable --now $UNIT"
fi
