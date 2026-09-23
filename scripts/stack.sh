#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROJECT=fintech-bank-platform
SERVICES=(account-service transaction-service payment-service notification-service api-gateway)
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8081}"
MAILPIT_URL="${MAILPIT_URL:-http://localhost:${MAILPIT_UI_PORT:-8025}}"

up() {
  docker compose -p "$PROJECT" -f "$ROOT/docker-compose.yml" up -d kafka cassandra redis mailpit
  docker compose -p "$PROJECT" -f "$ROOT/docker-compose.yml" up --exit-code-from kafka-init kafka-init
  for service in "${SERVICES[@]}"; do
    docker compose -f "$ROOT/services/$service/docker-compose.yml" up -d --build
  done
}

down() {
  for service in "${SERVICES[@]}"; do
    docker compose -f "$ROOT/services/$service/docker-compose.yml" down
  done
  docker compose -p "$PROJECT" -f "$ROOT/docker-compose.yml" down
}

wait_for() {
  local deadline=$((SECONDS + ${STACK_TIMEOUT:-600}))
  for url in "$GATEWAY_URL/health" http://localhost:8082/health http://localhost:8083/health http://localhost:8084/health http://localhost:8085/health "$MAILPIT_URL/livez"; do
    until curl -fsS "$url" >/dev/null 2>&1; do
      if [ "$SECONDS" -ge "$deadline" ]; then
        echo "timed out waiting for $url" >&2
        exit 1
      fi
      sleep 3
    done
    echo "ready: $url"
  done
}

logs() {
  for container in fintech-account-service fintech-transaction-service fintech-payment-service fintech-notification-service fintech-api-gateway; do
    echo "=== $container"
    docker logs --tail 200 "$container" 2>&1 || true
  done
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  wait) wait_for ;;
  logs) logs ;;
  *) echo "usage: $0 up|down|wait|logs" >&2; exit 2 ;;
esac
