#!/bin/sh
set -eu

if [ "$#" -gt 0 ]; then
  exec "$@"
fi

# /config is the desktop user's home. Commands from earlier Wktbox versions ran
# as root with HOME=/config and left root-owned files, such as Docker CLI
# state, that the desktop user can no longer read or update.
find /config -mindepth 1 -xdev -user 0 \
  -exec chown -h "${PUID:-1000}:${PGID:-1000}" {} + 2>/dev/null || true

/usr/local/bin/wktbox-init-nvm
exec /init
