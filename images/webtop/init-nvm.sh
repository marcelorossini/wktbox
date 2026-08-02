#!/bin/sh
set -eu

nvm_seed=/usr/local/share/nvm
nvm_dir=/config/.nvm
puid="${PUID:-1000}"
pgid="${PGID:-1000}"

if [ -e "$nvm_dir" ]; then
  exit 0
fi

nvm_tmp="${nvm_dir}.wktbox-tmp-$$"

install -d -m 0755 /config
install -d -m 0755 "$nvm_tmp"
trap 'rm -rf -- "$nvm_tmp"' EXIT HUP INT TERM
cp -a "$nvm_seed/." "$nvm_tmp/"
chown -R "$puid:$pgid" "$nvm_tmp"
mv "$nvm_tmp" "$nvm_dir"
trap - EXIT HUP INT TERM
