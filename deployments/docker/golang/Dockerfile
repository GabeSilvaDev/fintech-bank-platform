FROM golang:1.23-alpine

RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    openssh-client \
    tzdata

WORKDIR /workspace

CMD ["sh", "-lc", "tail -f /dev/null"]
