FROM golang:1.26.5 AS loopback-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY internal/interconnect ./internal/interconnect
COPY internal/lock ./internal/lock
COPY internal/loopback ./internal/loopback
COPY internal/portforward ./internal/portforward
COPY cmd/wktbox-loopback ./cmd/wktbox-loopback

RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/wktbox-loopback \
      ./cmd/wktbox-loopback

FROM docker:29.5.0-cli AS docker-cli

FROM lscr.io/linuxserver/webtop:ubuntu-xfce

ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/marcelorossini/wktbox

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      git \
      openssh-client \
    && rm -rf /var/lib/apt/lists/*

COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins /usr/local/libexec/docker/cli-plugins
COPY --from=loopback-build /out/wktbox-loopback /usr/local/bin/wktbox-loopback
COPY --chmod=0755 images/webtop/entrypoint.sh /usr/local/bin/wktbox-entrypoint

RUN docker --version \
    && docker compose version \
    && docker buildx version \
    && wktbox-loopback --help

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/wktbox-entrypoint"]

LABEL org.opencontainers.image.title="Wktbox Webtop" \
      org.opencontainers.image.description="Browser desktop and CLI runner for an isolated Wktbox DinD daemon" \
      org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses=Apache-2.0
