# ═══════════════════════════════════════════════════════════════════════════
# API Gateway - Makefile
# ═══════════════════════════════════════════════════════════════════════════

.PHONY: help test test-unit test-feature test-coverage test-verbose clean run build

# Default target
help:
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo "  API Gateway - Comandos Disponíveis"
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo ""
	@echo "  make test            - Roda todos os testes com cobertura"
	@echo "  make test-unit       - Roda apenas testes unitários"
	@echo "  make test-feature    - Roda apenas testes de feature"
	@echo "  make test-verbose    - Roda testes com output detalhado"
	@echo "  make test-coverage   - Gera relatório HTML de cobertura"
	@echo "  make clean           - Remove arquivos gerados"
	@echo "  make run             - Roda a aplicação localmente"
	@echo "  make build           - Compila a aplicação"
	@echo ""

# ═══════════════════════════════════════════════════════════════════════════
# Testes
# ═══════════════════════════════════════════════════════════════════════════

# Roda todos os testes com cobertura
test:
	@echo "🧪 Rodando todos os testes..."
	@go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/...
	@go tool cover -func=coverage.out | tail -1

# Roda apenas testes unitários
test-unit:
	@echo "🔬 Rodando testes unitários..."
	@go test ./tests/unit/... -v

# Roda apenas testes de feature
test-feature:
	@echo "🎯 Rodando testes de feature..."
	@go test ./tests/feature/... -v

# Roda testes com output verbose
test-verbose:
	@echo "📝 Rodando testes com output detalhado..."
	@go test ./tests/... ./cmd/... -v -coverprofile=coverage.out -coverpkg=./internal/...

# Gera relatório HTML de cobertura
test-coverage:
	@echo "📊 Gerando relatório de cobertura..."
	@go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/...
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out
	@echo ""
	@echo "✅ Relatório gerado: coverage.html"

# ═══════════════════════════════════════════════════════════════════════════
# Build & Run
# ═══════════════════════════════════════════════════════════════════════════

# Roda a aplicação
run:
	@go run cmd/main.go

# Compila a aplicação
build:
	@echo "🔨 Compilando..."
	@go build -o bin/api-gateway cmd/main.go
	@echo "✅ Binário gerado: bin/api-gateway"

# ═══════════════════════════════════════════════════════════════════════════
# Docker
# ═══════════════════════════════════════════════════════════════════════════

# Roda testes no container Docker
docker-test:
	@docker exec fintech-api-gateway go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/...
	@docker exec fintech-api-gateway go tool cover -func=coverage.out | tail -1

# Roda testes com cobertura HTML no Docker
docker-coverage:
	@docker exec fintech-api-gateway go test ./tests/... ./cmd/... -coverprofile=coverage.out -coverpkg=./internal/...
	@docker exec fintech-api-gateway go tool cover -func=coverage.out

# ═══════════════════════════════════════════════════════════════════════════
# Limpeza
# ═══════════════════════════════════════════════════════════════════════════

# Remove arquivos gerados
clean:
	@rm -f coverage.out coverage.html
	@rm -rf bin/
	@rm -rf tmp/
	@echo "🧹 Arquivos limpos"
