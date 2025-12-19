# 🏦 Fintech Bank Platform

A modern banking platform built with microservices architecture in Go, using Kafka for asynchronous communication (event-driven) and Cassandra for distributed persistence.

🇧🇷 [Leia em Português](#-fintech-bank-platform-1)

## 📋 Table of Contents

- [Architecture](#-architecture)
- [Technologies](#-technologies)
- [Prerequisites](#-prerequisites)
- [Quick Start](#-quick-start)
- [Project Structure](#-project-structure)
- [Services](#-services)
- [Infrastructure](#-infrastructure)
- [Roadmap](#-roadmap-sprints)
- [Contributing](#-contributing)

## 🏗 Architecture

```
                    ┌─────────────────┐
                    │   API Gateway   │
                    │  (HTTP → Kafka) │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │      Kafka      │
                    │  (KRaft Mode)   │
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
    ┌───▼───────┐    ┌──────▼──────┐    ┌───────▼─────┐
    │  Account  │    │ Transaction │    │   Payment   │
    │  Service  │    │   Service   │    │   Service   │
    │(Consumer) │    │ (Consumer)  │    │ (Consumer)  │
    └─────┬─────┘    └──────┬──────┘    └───────┬─────┘
          │                 │                    │
          └─────────────────┼────────────────────┘
                            │
                    ┌───────▼────────┐
                    │   Cassandra    │
                    │  (Single Node) │
                    └────────────────┘
```

### Data Flow

1. **API Gateway** receives HTTP requests and converts them to Kafka commands
2. **Kafka** distributes messages to consumer services
3. **Microservices** process commands and persist to Cassandra
4. **Redis** provides cache for frequent queries

## 🛠 Technologies

| Category | Technology | Version | Description |
|----------|------------|---------|-------------|
| **Language** | Go | 1.21+ | Main language |
| **Message Broker** | Apache Kafka | 3.7.1 | Async communication (KRaft mode) |
| **Database** | Apache Cassandra | 4.1 | Distributed persistence |
| **Cache** | Redis | 7.2-alpine | Cache and rate limiting |
| **HTTP Router** | Chi | 5.x | HTTP router for Go |
| **Containerization** | Docker Compose | 2.0+ | Local orchestration |

## 📋 Prerequisites

- [Docker](https://docs.docker.com/get-docker/) (v20.10+)
- [Docker Compose](https://docs.docker.com/compose/install/) (v2.0+)
- [Go](https://golang.org/dl/) (v1.21+)
- Minimum **4GB RAM** available for Docker

## 🚀 Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/GabeSilvaDev/fintech-bank-platform.git
cd fintech-bank-platform
```

### 2. Configure the environment

```bash
# Copy the environment variables file
cp .env.example .env
```

### 3. Start the infrastructure

```bash
# Start Kafka, Cassandra and Redis
docker compose up -d

# Wait for services to become healthy (~1-2 minutes)
docker compose ps
```

### 4. (Optional) Start debug UIs

```bash
# Kafka UI + Cassandra Web
docker compose --profile ui up -d
```

## 📁 Project Structure

```
fintech-bank-platform/
│
├── docker-compose.yml          # Container orchestration
├── .env.example                # Environment variables template
├── README.md                   # This file
│
├── pkg/                        # 📦 Shared packages
│   ├── logger/                 # Structured logger (zerolog)
│   ├── errors/                 # Standardized error handling
│   ├── response/               # HTTP response helpers
│   ├── validation/             # Shared validators
│   ├── events/                 # Kafka event definitions
│   ├── auth/                   # JWT helpers
│   └── dto/                    # Shared DTOs
│
├── scripts/                    # 🔧 Utility scripts
│
└── services/                   # 🎯 Microservices
    ├── api-gateway/            # HTTP → Kafka Gateway
    ├── account-service/        # Account management
    ├── transaction-service/    # Transaction processing
    ├── payment-service/        # Payments (PIX, TED, Boleto)
    └── notification-service/   # Notifications
```

### Microservice Structure

```
service-name/
├── cmd/
│   └── main.go                 # Entry point
├── internal/
│   ├── app/
│   │   ├── controllers/        # HTTP handlers
│   │   ├── services/           # Business logic
│   │   ├── repositories/       # Data access
│   │   ├── models/             # Domain models
│   │   ├── dto/                # DTOs
│   │   ├── validators/         # Validators
│   │   └── enums/              # Enumerations
│   ├── infrastructure/
│   │   ├── http/               # Router and server
│   │   ├── messaging/          # Kafka consumer/producer
│   │   └── database/           # Cassandra connection
│   └── config/                 # Configuration
├── migrations/                 # CQL migrations
└── tests/                      # Tests
    ├── unit/
    ├── integration/
    └── features/
```

## 🔧 Services (Planned)

| Service | Description | Port | Status |
|---------|-------------|------|--------|
| **API Gateway** | Receives HTTP and publishes to Kafka | 8081 | 🔜 Sprint 1 |
| **Account Service** | Account and user CRUD | 8082 | 🔜 Sprint 2 |
| **Transaction Service** | Transfer processing | 8083 | 🔜 Sprint 3 |
| **Payment Service** | PIX, TED, Boletos | 8084 | 🔜 Sprint 4 |
| **Notification Service** | Email, SMS, Push | 8085 | 🔜 Sprint 5 |

## 🏗️ Infrastructure

### Base Services

| Component | Container | Port | Description |
|-----------|-----------|------|-------------|
| Kafka | fintech-kafka | 9092 | Message broker (KRaft mode) |
| Cassandra | fintech-cassandra | 9042 | Database |
| Redis | fintech-redis | 6379 | Cache |

### Debug UIs (profile: ui)

| Component | Container | URL |
|-----------|-----------|-----|
| Kafka UI | fintech-kafka-ui | http://localhost:8080 |
| Cassandra Web | fintech-cassandra-web | http://localhost:3000 |

### Environment Variables

Copy the `.env.example` file to `.env`:

```bash
cp .env.example .env
```

| Variable | Description | Default |
|----------|-------------|---------|
| `KAFKA_BROKERS` | Kafka address | `localhost:9092` |
| `CASSANDRA_HOSTS` | Cassandra address | `localhost:9042` |
| `CASSANDRA_KEYSPACE` | Main keyspace | `fintech` |
| `REDIS_HOST` | Redis address | `localhost` |
| `REDIS_PORT` | Redis port | `6379` |

## 📅 Roadmap (Sprints)

| Sprint | Duration | Focus |
|--------|----------|-------|
| 0 | 1 week | ✅ Infrastructure + Docker Compose |
| 1 | 2 weeks | 🔜 API Gateway |
| 2 | 2 weeks | 🔜 Account Service |
| 3 | 2 weeks | 🔜 Transaction Service |
| 4 | 1.5 weeks | 🔜 Payment Service |
| 5 | 1 week | 🔜 Notification Service |
| 6 | 1.5 weeks | 🔜 E2E Tests + Performance |
| 7 | 1 week | 🔜 Docs + Observability |

## 🤝 Contributing

1. Fork the project
2. Create a branch for your feature (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

### Code Standards

- Follow [Effective Go](https://golang.org/doc/effective_go) conventions
- Keep test coverage above 95%
- Document public functions

## 📄 License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

---

<p align="center">
  Developed with ❤️ by <a href="https://github.com/GabeSilvaDev">Gabriel Silva</a>
</p>

---

# 🏦 Fintech Bank Platform

Uma plataforma bancária moderna construída com arquitetura de microserviços em Go, utilizando Kafka para comunicação assíncrona (event-driven) e Cassandra para persistência distribuída.

🇺🇸 [Read in English](#-fintech-bank-platform)

## 📋 Índice

- [Arquitetura](#-arquitetura)
- [Tecnologias](#-tecnologias)
- [Pré-requisitos](#-pré-requisitos)
- [Quick Start](#-quick-start-1)
- [Estrutura do Projeto](#-estrutura-do-projeto)
- [Serviços](#-serviços-planejados)
- [Infraestrutura](#-infraestrutura)
- [Roadmap](#-roadmap-sprints-1)
- [Contribuição](#-contribuição)

## 🏗 Arquitetura

```
                    ┌─────────────────┐
                    │   API Gateway   │
                    │  (HTTP → Kafka) │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │      Kafka      │
                    │  (KRaft Mode)   │
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
    ┌───▼───────┐    ┌──────▼──────┐    ┌───────▼─────┐
    │  Account  │    │ Transaction │    │   Payment   │
    │  Service  │    │   Service   │    │   Service   │
    │(Consumer) │    │ (Consumer)  │    │ (Consumer)  │
    └─────┬─────┘    └──────┬──────┘    └───────┬─────┘
          │                 │                    │
          └─────────────────┼────────────────────┘
                            │
                    ┌───────▼────────┐
                    │   Cassandra    │
                    │  (Single Node) │
                    └────────────────┘
```

### Fluxo de Dados

1. **API Gateway** recebe requisições HTTP e converte em comandos Kafka
2. **Kafka** distribui mensagens para os serviços consumidores
3. **Microserviços** processam comandos e persistem no Cassandra
4. **Redis** provê cache para consultas frequentes

## 🛠 Tecnologias

| Categoria | Tecnologia | Versão | Descrição |
|-----------|------------|--------|-----------|
| **Linguagem** | Go | 1.21+ | Linguagem principal |
| **Message Broker** | Apache Kafka | 3.7.1 | Comunicação assíncrona (KRaft mode) |
| **Banco de Dados** | Apache Cassandra | 4.1 | Persistência distribuída |
| **Cache** | Redis | 7.2-alpine | Cache e rate limiting |
| **HTTP Router** | Chi | 5.x | Router HTTP para Go |
| **Containerização** | Docker Compose | 2.0+ | Orquestração local |

## 📋 Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/) (v20.10+)
- [Docker Compose](https://docs.docker.com/compose/install/) (v2.0+)
- [Go](https://golang.org/dl/) (v1.21+)
- Mínimo de **4GB RAM** disponível para Docker

## 🚀 Quick Start

### 1. Clone o repositório

```bash
git clone https://github.com/GabeSilvaDev/fintech-bank-platform.git
cd fintech-bank-platform
```

### 2. Configure o ambiente

```bash
# Copie o arquivo de variáveis de ambiente
cp .env.example .env
```

### 3. Inicie a infraestrutura

```bash
# Inicia Kafka, Cassandra e Redis
docker compose up -d

# Aguarde os serviços ficarem healthy (~1-2 minutos)
docker compose ps
```

### 4. (Opcional) Inicie as UIs de debug

```bash
# Kafka UI + Cassandra Web
docker compose --profile ui up -d
```

## 📁 Estrutura do Projeto

```
fintech-bank-platform/
│
├── docker-compose.yml          # Orquestração de containers
├── .env.example                # Template de variáveis de ambiente
├── README.md                   # Este arquivo
│
├── pkg/                        # 📦 Pacotes compartilhados
│   ├── logger/                 # Logger estruturado (zerolog)
│   ├── errors/                 # Error handling padronizado
│   ├── response/               # HTTP response helpers
│   ├── validation/             # Validadores compartilhados
│   ├── events/                 # Definições de eventos Kafka
│   ├── auth/                   # JWT helpers
│   └── dto/                    # DTOs compartilhados
│
├── scripts/                    # 🔧 Scripts utilitários
│
└── services/                   # 🎯 Microserviços
    ├── api-gateway/            # Gateway HTTP → Kafka
    ├── account-service/        # Gerenciamento de contas
    ├── transaction-service/    # Processamento de transações
    ├── payment-service/        # Pagamentos (PIX, TED, Boleto)
    └── notification-service/   # Notificações
```

### Estrutura de cada Microserviço

```
service-name/
├── cmd/
│   └── main.go                 # Entry point
├── internal/
│   ├── app/
│   │   ├── controllers/        # HTTP handlers
│   │   ├── services/           # Business logic
│   │   ├── repositories/       # Data access
│   │   ├── models/             # Domain models
│   │   ├── dto/                # DTOs
│   │   ├── validators/         # Validadores
│   │   └── enums/              # Enumerações
│   ├── infrastructure/
│   │   ├── http/               # Router e server
│   │   ├── messaging/          # Kafka consumer/producer
│   │   └── database/           # Cassandra connection
│   └── config/                 # Configurações
├── migrations/                 # CQL migrations
└── tests/                      # Testes
    ├── unit/
    ├── integration/
    └── features/
```

## 🔧 Serviços (Planejados)

| Serviço | Descrição | Porta | Status |
|---------|-----------|-------|--------|
| **API Gateway** | Recebe HTTP e publica no Kafka | 8081 | 🔜 Sprint 1 |
| **Account Service** | CRUD de contas e usuários | 8082 | 🔜 Sprint 2 |
| **Transaction Service** | Processamento de transferências | 8083 | 🔜 Sprint 3 |
| **Payment Service** | PIX, TED, Boletos | 8084 | 🔜 Sprint 4 |
| **Notification Service** | Email, SMS, Push | 8085 | 🔜 Sprint 5 |

## 🏗️ Infraestrutura

### Serviços Base

| Componente | Container | Porta | Descrição |
|------------|-----------|-------|-----------|
| Kafka | fintech-kafka | 9092 | Message broker (KRaft mode) |
| Cassandra | fintech-cassandra | 9042 | Banco de dados |
| Redis | fintech-redis | 6379 | Cache |

### UIs de Debug (profile: ui)

| Componente | Container | URL |
|------------|-----------|-----|
| Kafka UI | fintech-kafka-ui | http://localhost:8080 |
| Cassandra Web | fintech-cassandra-web | http://localhost:3000 |

### Variáveis de Ambiente

Copie o arquivo `.env.example` para `.env`:

```bash
cp .env.example .env
```

| Variável | Descrição | Default |
|----------|-----------|---------|
| `KAFKA_BROKERS` | Endereço do Kafka | `localhost:9092` |
| `CASSANDRA_HOSTS` | Endereço do Cassandra | `localhost:9042` |
| `CASSANDRA_KEYSPACE` | Keyspace principal | `fintech` |
| `REDIS_HOST` | Endereço do Redis | `localhost` |
| `REDIS_PORT` | Porta do Redis | `6379` |

## 📅 Roadmap (Sprints)

| Sprint | Duração | Foco |
|--------|---------|------|
| 0 | 1 sem | ✅ Infraestrutura + Docker Compose |
| 1 | 2 sem | 🔜 API Gateway |
| 2 | 2 sem | 🔜 Account Service |
| 3 | 2 sem | 🔜 Transaction Service |
| 4 | 1.5 sem | 🔜 Payment Service |
| 5 | 1 sem | 🔜 Notification Service |
| 6 | 1.5 sem | 🔜 Testes E2E + Performance |
| 7 | 1 sem | 🔜 Docs + Observabilidade |

## 🤝 Contribuição

1. Fork o projeto
2. Crie uma branch para sua feature (`git checkout -b feature/AmazingFeature`)
3. Commit suas mudanças (`git commit -m 'Add some AmazingFeature'`)
4. Push para a branch (`git push origin feature/AmazingFeature`)
5. Abra um Pull Request

### Padrões de Código

- Siga as convenções do [Effective Go](https://golang.org/doc/effective_go)
- Mantenha cobertura de testes acima de 95%
- Documente funções públicas

## 📄 Licença

Este projeto está sob a licença MIT. Veja o arquivo [LICENSE](LICENSE) para mais detalhes.

---

<p align="center">
  Desenvolvido com ❤️ por <a href="https://github.com/GabeSilvaDev">Gabriel Silva</a>
</p>
