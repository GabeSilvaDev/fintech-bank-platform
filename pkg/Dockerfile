FROM golang:1.25-alpine

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod ./
RUN go mod download || true

CMD ["tail", "-f", "/dev/null"]
