.PHONY: help test test-unit test-feature test-integration test-coverage test-verbose clean run build

help:
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo "  Account Service"
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo ""
	@echo "  make test            - Run all tests with coverage"
	@echo "  make test-unit       - Run unit tests only"
	@echo "  make test-feature    - Run feature tests only"
	@echo "  make test-integration - Run Kafka and Cassandra integration tests (needs KAFKA_BROKERS and CASSANDRA_HOSTS)"
	@echo "  make test-verbose    - Run tests with verbose output"
	@echo "  make test-coverage   - Generate HTML coverage report"
	@echo "  make clean           - Remove generated files"
	@echo "  make run             - Run the service locally"
	@echo "  make build           - Build the binary"
	@echo ""

test:
	@echo "Running all tests..."
	@go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/app/...
	@go tool cover -func=coverage.out | tail -1

test-unit:
	@echo "Running unit tests..."
	@go test ./tests/unit/... -v

test-feature:
	@echo "Running feature tests..."
	@go test ./tests/feature/... -v

test-integration:
	@echo "Running integration tests..."
	@go test -tags integration ./tests/integration/... -v

test-verbose:
	@echo "Running tests (verbose)..."
	@go test ./tests/... ./cmd/... -v -coverprofile=coverage.out -coverpkg=./internal/app/...

test-coverage:
	@echo "Generating coverage report..."
	@go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/app/...
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out
	@echo ""
	@echo "Report written to coverage.html"

run:
	@go run cmd/main.go

build:
	@echo "Building..."
	@go build -o bin/account-service cmd/main.go
	@echo "Binary written to bin/account-service"

docker-test:
	@docker exec fintech-account-service go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/app/...
	@docker exec fintech-account-service go tool cover -func=coverage.out | tail -1

docker-coverage:
	@docker exec fintech-account-service go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/app/...
	@docker exec fintech-account-service go tool cover -func=coverage.out

clean:
	@rm -f coverage.out coverage.html
	@rm -rf bin/
	@rm -rf tmp/
	@echo "🧹 Arquivos limpos"
