.PHONY: help test test-coverage clean fmt lint docker-up docker-down docker-test docker-coverage

help:
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo "  pkg — shared packages"
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo ""
	@echo "  Local:"
	@echo "    make test            - Run all tests"
	@echo "    make test-coverage   - Generate coverage report"
	@echo "    make fmt             - Format code"
	@echo "    make lint            - Check formatting"
	@echo "    make clean           - Remove generated files"
	@echo ""
	@echo "  Docker:"
	@echo "    make docker-up       - Start the fintech-pkg container"
	@echo "    make docker-down     - Stop the fintech-pkg container"
	@echo "    make docker-test     - Run tests inside the container"
	@echo "    make docker-coverage - Gera coverage no container"
	@echo ""

test:
	@echo "Running tests..."
	@go test -v ./...

test-coverage:
	@echo "Generating coverage report..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out
	@echo ""
	@echo "Report written to coverage.html"

fmt:
	@echo "Formatting..."
	@gofmt -w .
	@echo "Formatted"

lint:
	@echo "Checking formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Files not gofmt-formatted:"; \
		gofmt -l .; \
		exit 1; \
	fi
	@echo "Formatting OK"

clean:
	@rm -f coverage.out coverage.html
	@echo "🧹 Arquivos limpos"

docker-up:
	@echo "🐳 Subindo container fintech-pkg..."
	@docker compose up -d --build
	@echo "✅ Container fintech-pkg rodando"

docker-down:
	@echo "🐳 Parando container fintech-pkg..."
	@docker compose down
	@echo "✅ Container parado"

docker-test:
	@echo "Running tests in the container..."
	@docker exec fintech-pkg go test -v -cover ./...

docker-coverage:
	@echo "📊 Gerando coverage no container..."
	@docker exec fintech-pkg sh -c "go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out"
