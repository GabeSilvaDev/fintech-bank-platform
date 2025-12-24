# ═══════════════════════════════════════════════════════════════════════════
# API Gateway - Development Dockerfile (Hot Reload)
# ═══════════════════════════════════════════════════════════════════════════

FROM golang:1.25-alpine

RUN apk add --no-cache git ca-certificates

RUN go install github.com/air-verse/air@latest

WORKDIR /app

# Copy go.mod only (download will happen at runtime with mounted volumes)
COPY go.mod go.sum ./

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]
