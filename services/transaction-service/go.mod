module github.com/fintech-bank-platform/transaction-service

go 1.25.0

require (
	github.com/apache/cassandra-gocql-driver/v2 v2.1.2
	github.com/fintech-bank-platform/pkg v0.0.0
	github.com/go-chi/chi/v5 v5.3.2
	github.com/google/uuid v1.6.0
	github.com/joho/godotenv v1.5.1
	github.com/segmentio/kafka-go v0.4.51
	github.com/stretchr/testify v1.11.1
	golang.org/x/sync v0.22.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/fintech-bank-platform/pkg => ../../pkg
