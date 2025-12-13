# 🚀 Fintech Event-Driven Platform  
## Plano Completo de Projeto — Golang + Apache Kafka + Microsserviços

> **Objetivo**: construir uma **fintech educacional** com arquitetura **orientada a eventos**, **microsserviços**, alta observabilidade e padrões usados em bancos digitais reais, utilizando **Golang** e **Apache Kafka**.

---

## 📌 1. Visão Geral

Este projeto simula um **banco digital** com:
- Cadastro de clientes (KYC)
- Contas bancárias
- Transações financeiras
- Ledger contábil (dupla entrada)
- Antifraude
- Notificações
- Analytics

### 🧠 Princípios Arquiteturais
- Event-Driven Architecture (EDA)
- Domain-Driven Design (DDD)
- Clean Architecture
- Microsserviços desacoplados
- Observabilidade by default
- Infra as Code

---

## 🧱 2. Stack Tecnológica

| Camada | Tecnologia |
|------|-----------|
| Linguagem | Go 1.23+ |
| API Gateway | Go + Fiber |
| Mensageria | Apache Kafka |
| Banco | Apache Cassandra |
| Auth | Keycloak (OIDC) |
| Observabilidade | OpenTelemetry |
| Logs | Grafana Loki |
| Tracing | Grafana Tempo |
| Métricas | Prometheus |
| Containers | Docker |
| Orquestração | Kubernetes (opcional) |
| Testes | Go testing + Testcontainers |

---

## 🧩 3. Arquitetura Geral (C4 – Nível 1)

Client → API Gateway → Auth → Microsserviços → Kafka → Cassandra
↘ Observability Stack ↙

---

## 🧩 4. Microsserviços do Domínio Bancário

| Serviço | Responsabilidade |
|------|------------------|
| api-gateway | Roteamento, auth, rate limit |
| auth-service | Autenticação e tokens |
| customer-service | Usuários e KYC |
| account-service | Contas e saldo |
| transaction-service | Transferências |
| ledger-service | Contabilidade (dupla entrada) |
| payment-service | Pagamentos externos |
| anti-fraud-service | Regras de fraude |
| notification-service | Emails e push |
| analytics-service | Métricas e relatórios |

---

## 🧩 5. Arquitetura Event-Driven (Kafka)

### 🔹 Tópicos Kafka
customer.created
customer.verified
account.created
account.balance.updated
transaction.initiated
transaction.validated
transaction.completed
transaction.failed
ledger.entry_written
payment.processed
fraud.alert
notification.send
analytics.event
dlq.*

### 🔹 Padrões Kafka
- Producer idempotente
- Consumer groups por serviço
- DLQ por tópico
- At-least-once delivery

---

## 🧱 6. Estrutura do Monorepo

fintech-platform/
│
├── api-gateway/
├── services/
│ ├── auth-service/
│ ├── customer-service/
│ ├── account-service/
│ ├── transaction-service/
│ ├── ledger-service/
│ ├── anti-fraud-service/
│ ├── payment-service/
│ ├── notification-service/
│ └── analytics-service/
│
├── shared/
│ ├── events/
│ ├── kafka/
│ ├── logger/
│ ├── otel/
│ └── config/
│
├── deployments/
│ ├── docker-compose.yml
│ └── k8s/
│
└── docs/
├── architecture/
├── srs.md
└── diagrams/


---

## 🧩 7. Estrutura de Cada Microsserviço (Padrão)

service-name/
├── cmd/
│ └── service-name/
│ └── main.go
│
├── internal/
│ ├── domain/
│ │ ├── model.go
│ │ ├── events.go
│ │ └── errors.go
│ │
│ ├── usecase/
│ │ ├── service.go
│ │ └── handlers.go
│ │
│ ├── repository/
│ │ ├── cassandra/
│ │ └── repository.go
│ │
│ ├── events/
│ │ ├── producer.go
│ │ └── consumer.go
│ │
│ ├── http/
│ │ └── routes.go
│ │
│ ├── observability/
│ │ ├── tracing.go
│ │ └── logging.go
│ │
│ └── config/
│
├── test/
│ ├── unit/
│ └── integration/
│
└── Dockerfile


---

## 🔐 8. Segurança

- OAuth2 / OIDC via Keycloak
- JWT com scopes
- Rate limiting no gateway
- Logs auditáveis
- Dados sensíveis criptografados

---

## 📊 9. Observabilidade

### Logs
- JSON estruturado
- Envio para Loki

### Tracing
- OpenTelemetry
- Spans por request e evento Kafka

### Métricas
- Latência por endpoint
- Consumo Kafka
- Erros por serviço

---

## 🧪 10. Testes

| Tipo | Ferramenta |
|---|---|
| Unitário | testing |
| Integração | Testcontainers |
| Contrato | Pact (opcional) |
| Load | k6 |

---

## 🗃️ 11. Modelo de Dados (Cassandra)

### customers_by_id

customer_id (PK)
name
email
document
verified
created_at

### accounts_by_id

account_id (PK)
customer_id
balance
blocked
created_at

### transactions_by_id

tx_id (PK)
from_account
to_account
amount
status
created_at

### ledger_entries

entry_id (PK)
tx_id
account_id
type (debit|credit)
amount
created_at

---

## 🔄 12. Fluxo de Negócio — Transferência

Client
→ API Gateway
→ Transaction Service
→ Kafka (transaction.initiated)
→ AntiFraud
→ Kafka (validated)
→ Ledger
→ Kafka (entry_written)
→ Account
→ Kafka (balance_updated)
→ Notification

---

## 🚀 13. Roadmap por Sprints

### Sprint 1 — Infra Base
- Docker Compose
- Kafka + Cassandra
- Observability stack

### Sprint 2 — Gateway + Auth
- API Gateway
- Keycloak
- JWT validation

### Sprint 3 — Customer + Account
- CRUD completo
- Eventos Kafka

### Sprint 4 — Transactions
- Transferências
- DLQ

### Sprint 5 — Ledger
- Dupla entrada
- Reconciliação

### Sprint 6 — AntiFraud
- Regras simples
- Alertas

### Sprint 7 — Notifications
- Email / logs

### Sprint 8 — Analytics
- Dashboards Grafana

---

## 🎯 14. Resultado Esperado

✔ Projeto sênior  
✔ Arquitetura bancária real  
✔ Event-driven na prática  
✔ Pronto para portfólio e entrevistas  
✔ Base sólida para estudos avançados  

---

## 📌 15. Próximos Passos

- Gerar templates automáticos
- Infra Kubernetes
- Chaos engineering
- Feature flags
- Versionamento de eventos

---

> **Este projeto demonstra domínio de arquitetura, Go, Kafka e sistemas financeiros reais.**