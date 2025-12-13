# 🏦 Fintech Event-Driven Platform

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go&logoColor=white)](https://golang.org/)
[![Kafka](https://img.shields.io/badge/Apache%20Kafka-4.0-231F20?style=flat&logo=apachekafka&logoColor=white)](https://kafka.apache.org/)
[![Cassandra](https://img.shields.io/badge/Cassandra-4.1-1287B1?style=flat&logo=apachecassandra&logoColor=white)](https://cassandra.apache.org/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker&logoColor=white)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

> A production-grade **educational fintech platform** built with **event-driven architecture**, **microservices**, and real-world banking patterns using **Golang** and **Apache Kafka**.

---

## 📋 Table of Contents

- [Overview](#-overview)
- [Architecture](#-architecture)
- [Tech Stack](#-tech-stack)
- [Microservices](#-microservices)
- [Getting Started](#-getting-started)
- [Project Structure](#-project-structure)
- [Kafka Topics](#-kafka-topics)
- [Data Model](#-data-model)
- [Contributing](#-contributing)
- [License](#-license)
- [Português](#-português)

---

## 🎯 Overview

This project simulates a **digital bank** with:

- 👤 Customer registration (KYC)
- 💳 Bank accounts management
- 💸 Financial transactions
- 📒 Double-entry ledger accounting
- 🛡️ Anti-fraud system
- 📧 Notifications
- 📊 Analytics & reporting

### Architectural Principles

- ✅ Event-Driven Architecture (EDA)
- ✅ Domain-Driven Design (DDD)
- ✅ Clean Architecture
- ✅ Decoupled Microservices
- ✅ Observability by Default
- ✅ Infrastructure as Code

---

## 🏗️ Architecture

```
┌─────────┐     ┌─────────────┐     ┌──────────────────┐
│ Client  │────▶│ API Gateway │────▶│ Auth (Keycloak)  │
└─────────┘     └──────┬──────┘     └────────┬─────────┘
                       │                     │
                       ▼                     ▼
              ┌────────────────────────────────────────┐
              │           Microservices                │
              │  ┌─────────┐  ┌─────────┐  ┌────────┐  │
              │  │Customer │  │Account  │  │  Tx    │  │
              │  │ Service │  │ Service │  │Service │  │
              │  └────┬────┘  └────┬────┘  └───┬────┘  │
              └───────┼───────────┼───────────┼───────┘
                      │           │           │
                      ▼           ▼           ▼
              ┌────────────────────────────────────────┐
              │            Apache Kafka                │
              │     (Event Bus / Message Broker)       │
              └───────────────────┬────────────────────┘
                                  │
                      ┌───────────┼───────────┐
                      ▼           ▼           ▼
              ┌──────────┐ ┌──────────┐ ┌──────────┐
              │  Ledger  │ │AntiFraud │ │Notifier  │
              │ Service  │ │ Service  │ │ Service  │
              └────┬─────┘ └──────────┘ └──────────┘
                   │
                   ▼
              ┌──────────┐
              │Cassandra │
              │(Database)│
              └──────────┘
```

### Business Flow — Transfer

```
Client → API Gateway → Transaction Service → Kafka (transaction.initiated)
    → AntiFraud → Kafka (validated) → Ledger → Kafka (entry_written)
    → Account → Kafka (balance_updated) → Notification
```

---

## 🛠️ Tech Stack

| Layer | Technology |
|-------|------------|
| **Language** | Go 1.23+ |
| **API Gateway** | Go + Fiber |
| **Messaging** | Apache Kafka (KRaft) |
| **Database** | Apache Cassandra |
| **Auth** | Keycloak (OIDC) |
| **Observability** | OpenTelemetry |
| **Logs** | Grafana Loki |
| **Tracing** | Grafana Tempo |
| **Metrics** | Prometheus |
| **Containers** | Docker |
| **Orchestration** | Kubernetes (optional) |
| **Testing** | Go testing + Testcontainers |

---

## 🔧 Microservices

| Service | Responsibility | Port |
|---------|---------------|------|
| `api-gateway` | Routing, auth, rate limiting | 8080 |
| `auth-service` | Authentication & tokens | 8081 |
| `customer-service` | Users & KYC | 8082 |
| `account-service` | Accounts & balance | 8083 |
| `transaction-service` | Transfers | 8084 |
| `ledger-service` | Double-entry accounting | 8085 |
| `anti-fraud-service` | Fraud rules & alerts | 8086 |
| `payment-service` | External payments | 8087 |
| `notification-service` | Emails & push | 8088 |
| `analytics-service` | Metrics & reports | 8089 |

---

## 🚀 Getting Started

### Prerequisites

- [Docker](https://www.docker.com/) & Docker Compose
- [Go 1.23+](https://golang.org/) (for development)

### Quick Start

1. **Clone the repository**

```bash
git clone https://github.com/your-username/fintech-bank-platform.git
cd fintech-bank-platform
```

2. **Start infrastructure** (Kafka, Cassandra, Kafka UI)

```bash
docker compose -f deployments/docker-compose.yml --profile infra up -d
```

3. **Start microservices**

```bash
docker compose -f deployments/docker-compose.yml --profile infra --profile app up -d --build
```

4. **Access services**

| Service | URL |
|---------|-----|
| API Gateway | http://localhost:8080 |
| Kafka UI | http://localhost:8090 |
| Kafka (broker) | localhost:29092 |
| Cassandra (CQL) | localhost:9042 |

### Stop & Cleanup

```bash
# Stop all containers
docker compose -f deployments/docker-compose.yml down

# Stop and remove volumes (reset data)
docker compose -f deployments/docker-compose.yml down -v
```

---

## 📁 Project Structure

```
fintech-bank-platform/
│
├── api-gateway/                 # API Gateway service
│   └── Dockerfile
│
├── services/
│   ├── auth-service/            # Authentication service
│   ├── customer-service/        # Customer/KYC service
│   ├── account-service/         # Account management
│   ├── transaction-service/     # Transaction processing
│   ├── ledger-service/          # Double-entry ledger
│   ├── anti-fraud-service/      # Fraud detection
│   ├── payment-service/         # External payments
│   ├── notification-service/    # Notifications
│   └── analytics-service/       # Analytics & reporting
│
├── shared/                      # Shared libraries
│   ├── events/                  # Event definitions
│   ├── kafka/                   # Kafka utilities
│   ├── logger/                  # Logging utilities
│   ├── otel/                    # OpenTelemetry setup
│   └── config/                  # Configuration
│
├── deployments/
│   ├── docker-compose.yml       # Docker Compose config
│   ├── docker/                  # Dockerfiles
│   └── k8s/                     # Kubernetes manifests
│
├── docs/                        # Documentation
│
├── SRS.md                       # Software Requirements Spec
└── README.md
```

### Microservice Internal Structure

```
service-name/
├── cmd/
│   └── service-name/
│       └── main.go              # Entry point
│
├── internal/
│   ├── domain/                  # Domain models & events
│   ├── usecase/                 # Business logic
│   ├── repository/              # Data access (Cassandra)
│   ├── events/                  # Kafka producer/consumer
│   ├── http/                    # HTTP handlers & routes
│   ├── observability/           # Tracing & logging
│   └── config/                  # Service configuration
│
├── test/
│   ├── unit/
│   └── integration/
│
├── Dockerfile
└── go.mod
```

---

## 📨 Kafka Topics

| Topic | Description |
|-------|-------------|
| `customer.created` | New customer registered |
| `customer.verified` | Customer KYC verified |
| `account.created` | New account created |
| `account.balance.updated` | Account balance changed |
| `transaction.initiated` | Transaction started |
| `transaction.validated` | Transaction passed anti-fraud |
| `transaction.completed` | Transaction finished |
| `transaction.failed` | Transaction failed |
| `ledger.entry_written` | Ledger entry recorded |
| `payment.processed` | External payment processed |
| `fraud.alert` | Fraud detected |
| `notification.send` | Notification requested |
| `analytics.event` | Analytics event |
| `dlq.*` | Dead letter queues |

### Kafka Patterns

- ✅ Idempotent producers
- ✅ Consumer groups per service
- ✅ Dead Letter Queue (DLQ) per topic
- ✅ At-least-once delivery

---

## 🗃️ Data Model (Cassandra)

### `customers_by_id`
| Column | Type |
|--------|------|
| customer_id (PK) | UUID |
| name | TEXT |
| email | TEXT |
| document | TEXT |
| verified | BOOLEAN |
| created_at | TIMESTAMP |

### `accounts_by_id`
| Column | Type |
|--------|------|
| account_id (PK) | UUID |
| customer_id | UUID |
| balance | DECIMAL |
| blocked | BOOLEAN |
| created_at | TIMESTAMP |

### `transactions_by_id`
| Column | Type |
|--------|------|
| tx_id (PK) | UUID |
| from_account | UUID |
| to_account | UUID |
| amount | DECIMAL |
| status | TEXT |
| created_at | TIMESTAMP |

### `ledger_entries`
| Column | Type |
|--------|------|
| entry_id (PK) | UUID |
| tx_id | UUID |
| account_id | UUID |
| type | TEXT (debit\|credit) |
| amount | DECIMAL |
| created_at | TIMESTAMP |

---

## 🔐 Security

- OAuth2 / OIDC via Keycloak
- JWT with scopes
- Rate limiting at gateway
- Auditable logs
- Encrypted sensitive data

---

## 📊 Observability

| Type | Tool | Description |
|------|------|-------------|
| **Logs** | Grafana Loki | Structured JSON logs |
| **Tracing** | Grafana Tempo | OpenTelemetry spans per request & Kafka event |
| **Metrics** | Prometheus | Latency, Kafka consumption, errors per service |

---

## 🧪 Testing

| Type | Tool |
|------|------|
| Unit | Go `testing` |
| Integration | Testcontainers |
| Contract | Pact (optional) |
| Load | k6 |

---

## 🤝 Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

# 🇧🇷 Português

## 🏦 Plataforma Fintech Event-Driven

> Uma **plataforma fintech educacional** de nível de produção, construída com **arquitetura orientada a eventos**, **microsserviços** e padrões bancários reais usando **Golang** e **Apache Kafka**.

---

## 🎯 Visão Geral

Este projeto simula um **banco digital** com:

- 👤 Cadastro de clientes (KYC)
- 💳 Gestão de contas bancárias
- 💸 Transações financeiras
- 📒 Ledger contábil (dupla entrada)
- 🛡️ Sistema antifraude
- 📧 Notificações
- 📊 Analytics e relatórios

### Princípios Arquiteturais

- ✅ Arquitetura Orientada a Eventos (EDA)
- ✅ Domain-Driven Design (DDD)
- ✅ Clean Architecture
- ✅ Microsserviços Desacoplados
- ✅ Observabilidade por Padrão
- ✅ Infraestrutura como Código

---

## 🚀 Começando

### Pré-requisitos

- [Docker](https://www.docker.com/) & Docker Compose
- [Go 1.23+](https://golang.org/) (para desenvolvimento)

### Início Rápido

1. **Clone o repositório**

```bash
git clone https://github.com/your-username/fintech-bank-platform.git
cd fintech-bank-platform
```

2. **Inicie a infraestrutura** (Kafka, Cassandra, Kafka UI)

```bash
docker compose -f deployments/docker-compose.yml --profile infra up -d
```

3. **Inicie os microsserviços**

```bash
docker compose -f deployments/docker-compose.yml --profile infra --profile app up -d --build
```

4. **Acesse os serviços**

| Serviço | URL |
|---------|-----|
| API Gateway | http://localhost:8080 |
| Kafka UI | http://localhost:8090 |
| Kafka (broker) | localhost:29092 |
| Cassandra (CQL) | localhost:9042 |

### Parar e Limpar

```bash
# Parar todos os containers
docker compose -f deployments/docker-compose.yml down

# Parar e remover volumes (resetar dados)
docker compose -f deployments/docker-compose.yml down -v
```

---

## 🔧 Microsserviços

| Serviço | Responsabilidade | Porta |
|---------|-----------------|-------|
| `api-gateway` | Roteamento, auth, rate limit | 8080 |
| `auth-service` | Autenticação e tokens | 8081 |
| `customer-service` | Usuários e KYC | 8082 |
| `account-service` | Contas e saldo | 8083 |
| `transaction-service` | Transferências | 8084 |
| `ledger-service` | Contabilidade (dupla entrada) | 8085 |
| `anti-fraud-service` | Regras de fraude | 8086 |
| `payment-service` | Pagamentos externos | 8087 |
| `notification-service` | Emails e push | 8088 |
| `analytics-service` | Métricas e relatórios | 8089 |

---

<p align="center">
  Made with ❤️ for the developer community
</p>
