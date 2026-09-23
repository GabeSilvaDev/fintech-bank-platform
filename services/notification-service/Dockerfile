FROM golang:1.25-alpine

RUN apk add --no-cache git ca-certificates

RUN go install github.com/air-verse/air@v1.67.0

WORKDIR /app

COPY go.mod go.sum ./

COPY . .

EXPOSE 8085

CMD ["air", "-c", ".air.toml"]
