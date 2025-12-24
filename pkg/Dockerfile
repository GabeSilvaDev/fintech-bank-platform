# ═══════════════════════════════════════════════════════════════════════════
# PKG - Shared Packages Dockerfile
# ═══════════════════════════════════════════════════════════════════════════

FROM golang:1.25-alpine

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod ./
RUN go mod download || true

# Container para desenvolvimento e testes
CMD ["tail", "-f", "/dev/null"]
