FROM nginx:1.31.3-alpine

COPY nginx.conf.template /etc/nginx/conf.d/default.conf

RUN nginx -t

EXPOSE 8080

LABEL org.opencontainers.image.title="Wktbox Gateway" \
      org.opencontainers.image.description="Loopback-only reverse proxy for services published by a Wktbox DinD daemon"
