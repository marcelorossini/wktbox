FROM docker:29.5.0-cli AS docker-cli

FROM lscr.io/linuxserver/webtop:ubuntu-xfce

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
COPY --chmod=0755 entrypoint.sh /usr/local/bin/wktbox-entrypoint

RUN docker --version \
    && docker compose version \
    && docker buildx version

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/wktbox-entrypoint"]

LABEL org.opencontainers.image.title="Wktbox Webtop" \
      org.opencontainers.image.description="Browser desktop and CLI runner for an isolated Wktbox DinD daemon"
