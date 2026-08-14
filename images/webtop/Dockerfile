FROM golang:1.27rc2 AS loopback-build

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

FROM docker:29.7.2-cli AS docker-cli

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
      socat \
    && rm -rf /var/lib/apt/lists/*

COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins /usr/local/libexec/docker/cli-plugins
COPY --from=loopback-build /out/wktbox-loopback /usr/local/bin/wktbox-loopback
COPY --chmod=0755 images/webtop/entrypoint.sh /usr/local/bin/wktbox-entrypoint
COPY --chmod=0755 images/webtop/wrapped-chromium /usr/local/bin/wrapped-chromium
COPY --chmod=0755 images/webtop/chromium-supervisor /usr/local/bin/wktbox-chromium-supervisor
COPY --chmod=0644 images/webtop/wktbox-chromium.desktop /etc/xdg/autostart/wktbox-chromium.desktop
COPY --chmod=0755 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/run /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/run
COPY --chmod=0644 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/type /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/type
COPY --chmod=0644 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/dependencies.d/init-services /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/dependencies.d/init-services
COPY --chmod=0644 images/webtop/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay /etc/s6-overlay/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay

RUN docker --version \
    && docker compose version \
    && docker buildx version \
    && wktbox-loopback --help

RUN test -x /usr/local/bin/wrapped-chromium \
    && test -x /usr/local/bin/wktbox-chromium-supervisor \
    && test -r /etc/xdg/autostart/wktbox-chromium.desktop \
    && command -v socat \
    && test -x /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/run

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/wktbox-entrypoint"]

LABEL org.opencontainers.image.title="Wktbox Webtop" \
      org.opencontainers.image.description="Browser desktop and CLI runner for an isolated Wktbox DinD daemon" \
      org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses=Apache-2.0
