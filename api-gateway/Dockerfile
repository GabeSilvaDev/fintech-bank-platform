FROM golang:1.23-alpine

RUN apk add --no-cache ca-certificates git bash
WORKDIR /app

# Quando você adicionar o código, ajuste o build para ./cmd/api-gateway
# e garanta que exista um go.mod dentro deste diretório.

EXPOSE 8080
CMD ["sh","-lc","go run ./cmd/api-gateway"]
