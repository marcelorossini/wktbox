#!/bin/sh
set -eu

if [ "$#" -gt 0 ]; then
  exec "$@"
fi

/usr/local/bin/wktbox-init-nvm
exec /init
