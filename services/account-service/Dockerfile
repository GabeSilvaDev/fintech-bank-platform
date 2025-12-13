FROM golang:1.23-alpine

RUN apk add --no-cache ca-certificates git bash
WORKDIR /app

EXPOSE 8083
CMD ["sh","-lc","go run ./cmd/account-service"]
