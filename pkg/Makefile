# ═══════════════════════════════════════════════════════════════════════════
# PKG - Makefile
# ═══════════════════════════════════════════════════════════════════════════

.PHONY: help test test-coverage clean fmt lint docker-up docker-down docker-test docker-coverage

help:
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo "  PKG - Pacotes Compartilhados - Comandos Disponíveis"
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo ""
	@echo "  Local:"
	@echo "    make test            - Roda todos os testes"
	@echo "    make test-coverage   - Gera relatório de cobertura"
	@echo "    make fmt             - Formata o código"
	@echo "    make lint            - Verifica formatação"
	@echo "    make clean           - Remove arquivos gerados"
	@echo ""
	@echo "  Docker:"
	@echo "    make docker-up       - Sobe container fintech-pkg"
	@echo "    make docker-down     - Para container fintech-pkg"
	@echo "    make docker-test     - Roda testes no container"
	@echo "    make docker-coverage - Gera coverage no container"
	@echo ""

test:
	@echo "🧪 Rodando testes..."
	@go test -v ./...

test-coverage:
	@echo "📊 Gerando relatório de cobertura..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out
	@echo ""
	@echo "✅ Relatório gerado: coverage.html"

fmt:
	@echo "🎨 Formatando código..."
	@gofmt -w .
	@echo "✅ Código formatado"

lint:
	@echo "🔍 Verificando formatação..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "❌ Código não formatado:"; \
		gofmt -l .; \
		exit 1; \
	fi
	@echo "✅ Código formatado corretamente"

clean:
	@rm -f coverage.out coverage.html
	@echo "🧹 Arquivos limpos"

# ═══════════════════════════════════════════════════════════════════════════
# DOCKER COMMANDS
# ═══════════════════════════════════════════════════════════════════════════

docker-up:
	@echo "🐳 Subindo container fintech-pkg..."
	@docker compose up -d --build
	@echo "✅ Container fintech-pkg rodando"

docker-down:
	@echo "🐳 Parando container fintech-pkg..."
	@docker compose down
	@echo "✅ Container parado"

docker-test:
	@echo "🧪 Rodando testes no container..."
	@docker exec fintech-pkg go test -v -cover ./...

docker-coverage:
	@echo "📊 Gerando coverage no container..."
	@docker exec fintech-pkg sh -c "go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out"
