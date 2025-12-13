FROM golang:1.23-alpine

RUN apk add --no-cache ca-certificates git bash
WORKDIR /app

EXPOSE 8089
CMD ["sh","-lc","go run ./cmd/analytics-service"]
