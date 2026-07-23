FROM docker:29.5.0-cli

RUN apk add --no-cache \
      bash \
      curl \
      git \
      openssh-client \
      tini

WORKDIR /workspace

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["sleep", "infinity"]
