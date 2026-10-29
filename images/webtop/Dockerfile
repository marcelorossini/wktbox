FROM golang:1.26.5@sha256:705e964a93a2fd2e75c7d59bb7d781b57e30f12293ffde5175c69229e18fb678 AS loopback-build

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

FROM docker:29.5.0-cli@sha256:864f36643a29d212f6cc88ec3aa1ebd2d939283df933c4c5c1074a982237f9bd AS docker-cli

FROM lscr.io/linuxserver/webtop:ubuntu-xfce@sha256:432475b977d9ff06f8f8adfb9608541bbde51a566ac0cbe62af44f33265e1a37

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      bash \
      build-essential \
      ca-certificates \
      curl \
      git \
      gnupg \
      jq \
      openssh-client \
      pipx \
      python-is-python3 \
      python3 \
      python3-pip \
      python3-venv \
      ripgrep \
      socat \
      unzip \
      zip \
    && install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
      | gpg --dearmor --batch --yes -o /etc/apt/keyrings/microsoft.gpg \
    && chmod a+r /etc/apt/keyrings/microsoft.gpg \
    && printf '%s\n' \
      'Types: deb' \
      'URIs: https://packages.microsoft.com/repos/code' \
      'Suites: stable' \
      'Components: main' \
      'Architectures: amd64 arm64' \
      'Signed-By: /etc/apt/keyrings/microsoft.gpg' \
      > /etc/apt/sources.list.d/vscode.sources \
    && curl -fsSL https://dbeaver.io/debs/dbeaver.gpg.key \
      | gpg --dearmor --batch --yes -o /etc/apt/keyrings/dbeaver.gpg \
    && chmod a+r /etc/apt/keyrings/dbeaver.gpg \
    && printf '%s\n' \
      'deb [arch=amd64,arm64 signed-by=/etc/apt/keyrings/dbeaver.gpg] https://dbeaver.io/debs/dbeaver-ce /' \
      > /etc/apt/sources.list.d/dbeaver.list \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      code \
      dbeaver-ce \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ARG NVM_VERSION=v0.40.4
ARG NODE_VERSION=lts/*

RUN git clone --branch "$NVM_VERSION" --depth 1 \
      https://github.com/nvm-sh/nvm.git /usr/local/share/nvm \
    && export NVM_DIR=/usr/local/share/nvm \
    && . "$NVM_DIR/nvm.sh" \
    && nvm install "$NODE_VERSION" \
    && nvm alias default "$NODE_VERSION" \
    && { command -v corepack >/dev/null || npm install --global corepack; } \
    && nvm cache clear \
    && npm cache clean --force \
    && rm -rf "$NVM_DIR/.git" \
    && test "$(nvm --version)" = "${NVM_VERSION#v}" \
    && node --version \
    && npm --version \
    && corepack --version

COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins /usr/local/libexec/docker/cli-plugins
COPY --from=loopback-build /out/wktbox-loopback /usr/local/bin/wktbox-loopback
COPY --chmod=0755 images/webtop/entrypoint.sh /usr/local/bin/wktbox-entrypoint
COPY --chmod=0755 images/webtop/init-nvm.sh /usr/local/bin/wktbox-init-nvm
COPY --chmod=0755 images/webtop/wktbox-exec /usr/local/bin/wktbox-exec
COPY --chmod=0644 images/webtop/wktbox-nvm.sh /etc/profile.d/wktbox-nvm.sh
COPY --chmod=0755 images/webtop/wrapped-chromium /usr/local/bin/wrapped-chromium
COPY --chmod=0755 images/webtop/wrapped-code /usr/local/bin/code
COPY --chmod=0755 images/webtop/chromium-supervisor /usr/local/bin/wktbox-chromium-supervisor
COPY --chmod=0644 images/webtop/wktbox-chromium.desktop /etc/xdg/autostart/wktbox-chromium.desktop
COPY --chmod=0755 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/run /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/run
COPY --chmod=0644 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/type /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/type
COPY --chmod=0644 images/webtop/s6-rc.d/svc-wktbox-cdp-relay/dependencies.d/init-services /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/dependencies.d/init-services
COPY --chmod=0644 images/webtop/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay /etc/s6-overlay/s6-rc.d/user/contents.d/svc-wktbox-cdp-relay

RUN sed -i 's#^Exec=/usr/share/code/code#Exec=/usr/local/bin/code#' \
      /usr/share/applications/com.microsoft.VSCode.desktop \
      /usr/share/applications/com.microsoft.VSCode.UrlHandler.desktop \
    && ! grep -h '^Exec=/usr/share/code/code' /usr/share/applications/com.microsoft.VSCode*.desktop

RUN printf '%s\n' \
      '' \
      '# Wktbox persistent NVM environment' \
      '[ -r /etc/profile.d/wktbox-nvm.sh ] && . /etc/profile.d/wktbox-nvm.sh' \
      >> /etc/bash.bashrc

RUN docker --version \
    && docker compose version \
    && docker buildx version \
    && wktbox-loopback --help \
    && python --version \
    && python3 --version \
    && pip --version \
    && pipx --version \
    && code --version --no-sandbox --user-data-dir=/tmp/wktbox-code-build-test \
    && command -v dbeaver \
    && command -v cc \
    && command -v jq \
    && command -v rg

RUN test -x /usr/local/bin/wrapped-chromium \
    && test -x /usr/local/bin/wktbox-chromium-supervisor \
    && test -x /usr/local/bin/wktbox-init-nvm \
    && test -x /usr/local/bin/wktbox-exec \
    && test -r /etc/profile.d/wktbox-nvm.sh \
    && test -s /usr/local/share/nvm/nvm.sh \
    && test -r /etc/xdg/autostart/wktbox-chromium.desktop \
    && command -v socat \
    && test -x /etc/s6-overlay/s6-rc.d/svc-wktbox-cdp-relay/run

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/wktbox-entrypoint"]

ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/marcelorossini/wktbox

LABEL org.opencontainers.image.title="Wktbox Webtop" \
      org.opencontainers.image.description="Browser desktop and CLI runner for an isolated Wktbox DinD daemon" \
      org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses=Apache-2.0
