#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
version="9.8.7"
revision="0123456789abcdef0123456789abcdef01234567"
source_url="https://example.invalid/wktbox"

docker build \
  --build-arg "VERSION=$version" \
  --build-arg "REVISION=$revision" \
  --build-arg "SOURCE_URL=$source_url" \
  --file "$project_root/images/webtop/Dockerfile" \
  --tag wktbox/webtop:metadata-test \
  "$project_root"

docker build \
  --build-arg "VERSION=$version" \
  --build-arg "REVISION=$revision" \
  --build-arg "SOURCE_URL=$source_url" \
  --tag wktbox/gateway:metadata-test \
  "$project_root/images/gateway"

label() {
  local image="$1"
  local name="$2"
  docker image inspect \
    --format "{{ index .Config.Labels \"$name\" }}" \
    "$image"
}

for image in wktbox/webtop:metadata-test wktbox/gateway:metadata-test; do
  test "$(label "$image" org.opencontainers.image.source)" = "$source_url"
  test "$(label "$image" org.opencontainers.image.revision)" = "$revision"
  test "$(label "$image" org.opencontainers.image.version)" = "$version"
  test "$(label "$image" org.opencontainers.image.licenses)" = "Apache-2.0"
  test -n "$(label "$image" org.opencontainers.image.description)"
done

printf 'runtime image OCI label tests passed\n'
