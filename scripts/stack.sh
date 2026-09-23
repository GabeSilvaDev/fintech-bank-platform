#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROJECT=fintech-bank-platform
SERVICES=(account-service transaction-service payment-service notification-service api-gateway)
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8081}"
MAILPIT_URL="${MAILPIT_URL:-http://localhost:${MAILPIT_UI_PORT:-8025}}"
OBSERVABILITY="${STACK_OBSERVABILITY:-0}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:${PROMETHEUS_PORT:-9090}}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:${GRAFANA_PORT:-3000}}"
JAEGER_URL="${JAEGER_URL:-http://localhost:${JAEGER_UI_PORT:-16686}}"

observability() {
  [ "$OBSERVABILITY" = "1" ]
}

root_compose() {
  if observability; then
    docker compose -p "$PROJECT" -f "$ROOT/docker-compose.yml" --profile observability "$@"
  else
    docker compose -p "$PROJECT" -f "$ROOT/docker-compose.yml" "$@"
  fi
}

up() {
  root_compose up -d kafka cassandra redis mailpit
  root_compose up --exit-code-from kafka-init kafka-init
  if observability; then
    root_compose up -d prometheus grafana jaeger
    export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://jaeger:4318}"
  fi
  for service in "${SERVICES[@]}"; do
    docker compose -f "$ROOT/services/$service/docker-compose.yml" up -d --build
  done
}

down() {
  for service in "${SERVICES[@]}"; do
    docker compose -f "$ROOT/services/$service/docker-compose.yml" down
  done
  root_compose down
}

wait_for() {
  local deadline=$((SECONDS + ${STACK_TIMEOUT:-600}))
  local urls=("$GATEWAY_URL/health" http://localhost:8082/health http://localhost:8083/health http://localhost:8084/health http://localhost:8085/health "$MAILPIT_URL/livez")
  if observability; then
    urls+=("$PROMETHEUS_URL/-/ready" "$GRAFANA_URL/api/health" "$JAEGER_URL/")
  fi
  for url in "${urls[@]}"; do
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
