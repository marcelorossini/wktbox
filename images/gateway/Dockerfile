FROM nginx:1.31.3-alpine

ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/marcelorossini/wktbox

COPY nginx.conf.template /etc/nginx/conf.d/default.conf

RUN nginx -t

EXPOSE 8080

LABEL org.opencontainers.image.title="Wktbox Gateway" \
      org.opencontainers.image.description="Loopback-only reverse proxy for services published by a Wktbox DinD daemon" \
      org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses=Apache-2.0
