<div align="center">

# Fintech Bank Platform

**Backend bancário event-driven em Go** — um API gateway HTTP publicando comandos no Kafka, microserviços de domínio consumindo, Cassandra para persistência e Redis para cache.

[![Status](https://img.shields.io/badge/status-em%20desenvolvimento-f59e0b)](#roadmap)
[![CI](https://github.com/GabeSilvaDev/fintech-bank-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/GabeSilvaDev/fintech-bank-platform/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Kafka](https://img.shields.io/badge/Kafka-3.7%20KRaft-231F20?logo=apachekafka&logoColor=white)](https://kafka.apache.org)
[![Cassandra](https://img.shields.io/badge/Cassandra-4.1-1287B1?logo=apachecassandra&logoColor=white)](https://cassandra.apache.org)
[![Redis](https://img.shields.io/badge/Redis-7.2-DC382D?logo=redis&logoColor=white)](https://redis.io)
[![Cobertura](https://img.shields.io/badge/cobertura-100%25%20exigida-2e7d32)](#desenvolvimento)
[![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-555)](LICENSE)

[English](README.md) · **Português (Brasil)**

</div>

> **Em desenvolvimento.** Infraestrutura, pacotes compartilhados e os cinco serviços — o API Gateway, o Account Service, o Transaction Service, o Payment Service e o Notification Service — estão prontos: os comandos fluem do HTTP para o Kafka e para o Cassandra, depósitos, saques e transferências se resolvem como sagas sobre o account service, pagamentos por PIX, TED e boleto se resolvem da mesma forma através de um provedor sandbox com webhooks assinados, e os eventos de resultado viram e-mail, SMS e push através do notification service, com as leituras voltando pelo gateway. Testes end-to-end, testes de carga e observabilidade vêm a seguir. Veja o [roadmap](#roadmap) para o que está feito e o que está planejado.

## Arquitetura

```mermaid
flowchart LR
    C[Cliente] -->|HTTP| GW[API Gateway<br/>Go · Chi]
    GW -->|comandos| K[(Kafka<br/>KRaft)]
    K --> A[Account Service]
    K --> T[Transaction Service]
    K --> P[Payment Service]
    K --> N[Notification Service]
    A & T & P --> CS[(Cassandra)]
    N --> R[(Redis)]
    N --> MP[(Mailpit)]
    A & T & P & N -->|eventos| K
```

O gateway recebe requisições HTTP e publica-as como comandos no Kafka; cada serviço de domínio consome seu tópico de comandos, persiste no Cassandra e emite eventos de resultado. O Notification Service transforma esses eventos de resultado em e-mail, SMS e push, usando o Redis para idempotência e histórico e o Mailpit para capturar os e-mails enviados em desenvolvimento.

**Tópicos** (`pkg/events`): `account.commands`, `transaction.commands`, `payment.commands` para comandos; `account.events`, `transaction.events`, `payment.events`, `notification.events` para resultados; um tópico de dead-letter por domínio. O compose raiz pré-cria todos os tópicos com um one-shot `kafka-init`.

## O que existe hoje

| Componente | Caminho | Estado |
|---|---|---|
| Infraestrutura | `docker-compose.yml` | Kafka 3.7.1 (KRaft), Cassandra 4.1, Redis 7.2, Mailpit, Kafka UI e Cassandra Web opcionais |
| Pacotes compartilhados | `pkg/` | `logger`, `errors`, `response`, `validation`, `events`, `env`, `middleware`, `messaging`, `domain`, `cassandra`, `processor` — 100 % de cobertura, exigida no CI |
| API Gateway | `services/api-gateway/` | Router Chi com middlewares de request-id, real-IP, logging, recovery, CORS e rate limit; `GET /health`; endpoints de comando publicando no Kafka através de um producer protegido por circuit breaker; config tipada a partir do ambiente; testes unitários + de feature com 100 % de cobertura, teste de integração com Kafka no CI; rotas de leitura repassadas por proxy ao account service |
| Account Service | `services/account-service/` | Consome `account.commands`, persiste clientes e contas no Cassandra (`fintech_accounts`, migrations aplicadas no boot), controla os saldos com créditos/débitos em compare-and-set, publica resultados — incluindo `account.credit_rejected` — em `account.events` e falhas em `account.dlq`; API de leitura na `:8082`; testes unitários + de feature com 100 % de `internal/app`, testes de integração com Cassandra e Kafka no CI |
| Transaction Service | `services/transaction-service/` | Consome `transaction.commands` e as respostas do account service em `account.events`, registra depósitos, saques e transferências no Cassandra (`fintech_transactions`, migrations aplicadas no boot), orquestra cada um como uma saga sobre `account.commands` (débito → crédito → crédito compensatório em caso de falha) com chaves de idempotência por etapa, publica `transaction.created/completed/failed` e `transaction.transfer_completed/transfer_failed` em `transaction.events`, envia para dead-letter em `transaction.dlq`; API de leitura na `:8083`; testes unitários + de feature com 100 % de `internal/app`, testes de integração com Cassandra e Kafka no CI |
| Payment Service | `services/payment-service/` | Consome `payment.commands` e as respostas do account service em `account.events`, guarda pagamentos por PIX, TED e boleto no Cassandra (`fintech_payments`, migrations aplicadas no boot), reserva os fundos com `account.debit`, submete a um provedor sandbox — PIX se resolve na hora, TED e boleto se resolvem por um webhook assinado — estorna rejeições com `account.credit`, publica `payment.created/processed/completed/failed` em `payment.events`, envia para dead-letter em `payment.dlq`; API de leitura e webhook na `:8084`; testes unitários + de feature com 100 % de `internal/app`, testes de integração com Cassandra e Kafka no CI |
| Notification Service | `services/notification-service/` | Consome `account.events`, `transaction.events` e `payment.events` e transforma os resultados em e-mail, SMS e push em português, buscando os contatos no endpoint interno de titular do account service; os comandos de entrega em `notification.events` são enviados por SMTP (Mailpit em desenvolvimento) ou por provedores sandbox de SMS/push, registrados num histórico apoiado em Redis, e enviados para dead-letter em `notification.dlq`; API de leitura na `:8085`; testes unitários + de feature com 100 % de `internal/app`, testes de integração com Kafka, Redis e Mailpit no CI |

### Pacotes compartilhados

| Pacote | O que dá a cada serviço |
|---|---|
| `logger` | Wrapper do zerolog com `Config{Level, Pretty, TimeFormat, Output}` e presets de desenvolvimento/produção |
| `errors` | `AppError` com código, mensagem, status HTTP e detalhes; construtores por status (`BadRequest`, `NotFound`, …) |
| `response` | Helpers JSON (`OK`, `Created`, `NoContent`, `BadRequest`, …), `SuccessWithMeta` para paginação, `FromError` para renderizar um `AppError` |
| `validation` | Validadores brasileiros e bancários — CPF, CNPJ, telefone, chave PIX, agência e conta, moeda, força de senha — como funções e como tags `validate:"…"` |
| `events` | Envelope `Event` do Kafka (id, tipo, versão, origem, timestamp, trace id, metadata, payload), catálogos de tópicos e tipos de evento, payloads tipados e construtores como `NewAccountCommand` |
| `env` | Getters tipados para variáveis de ambiente |
| `middleware` | Request-id, logging de requisições e recovery de panics para o chi |
| `messaging` | Producer kafka-go com timeout de publicação e um loop de consumer com commit por mensagem que termina a mensagem em andamento no shutdown (`DrainTimeout`) e reinicia com backoff (`RunWithRestart`) |
| `domain` | Erros de domínio compartilhados (`ErrNotFound`, `ErrConflict`, `ErrAmbiguousWrite`, `Invalid`/`IsInvalid`/`InvalidCode`) e helpers de dinheiro (`ToCents`, `FromCents`, `Cents`) |
| `cassandra` | `Migrator` que roda primeiro o arquivo do keyspace e depois cada `.cql` em ordem sobre uma interface `Executor`, registrando as versões em `schema_migrations`; `MapWriteError` mapeia falhas ambíguas do Cassandra para `domain.ErrAmbiguousWrite` |
| `processor` | Processador idempotente de comandos Kafka: deduplica pelo id do evento, retenta erros transitórios com backoff, envia o resto para dead-letter e publica os eventos de resposta de um dispatcher |

Exemplos de uso em [`pkg/README.md`](pkg/README.md).

## Como rodar

Requer Docker, Docker Compose e ~4 GB de RAM para os containers. Go 1.25 só se quiser rodar os serviços fora do Docker.

```bash
git clone https://github.com/GabeSilvaDev/fintech-bank-platform.git
cd fintech-bank-platform
cp .env.example .env

docker compose up -d                  # Kafka + Cassandra + Redis
docker compose --profile ui up -d     # + Kafka UI (:8080) e Cassandra Web (:3000)
docker compose ps                     # espere tudo ficar healthy (~1–2 min)
```

| Serviço | Container | Porta |
|---|---|---|
| Kafka (KRaft) | `fintech-kafka` | 9092 |
| Cassandra | `fintech-cassandra` | 9042 |
| Redis | `fintech-redis` | 6379 (`REDIS_PORT`) |
| Mailpit | `fintech-mailpit` | 1025 SMTP (`MAILPIT_SMTP_PORT`) · 8025 UI (`MAILPIT_UI_PORT`) |
| Kafka UI *(profile `ui`)* | `fintech-kafka-ui` | 8080 |
| Cassandra Web *(profile `ui`)* | `fintech-cassandra-web` | 3000 |

O Redis sustenta a idempotência e o histórico do notification service; o Mailpit captura os e-mails enviados por ele, com interface web em http://localhost:8025.

### API Gateway

```bash
cd services/api-gateway
cp .env.example .env
docker compose up -d                  # hot reload com Air, publicado em :8081
curl http://localhost:8081/health
```

Ou nativo: `make run` (escuta em `SERVER_PORT`, padrão 8080). A configuração vem do ambiente: `SERVER_*` (host, porta, timeouts), `CORS_*`, `RATE_LIMIT_REQUESTS` / `RATE_LIMIT_WINDOW`, `KAFKA_BROKERS` / `KAFKA_WRITE_TIMEOUT` / `KAFKA_BATCH_TIMEOUT` / `KAFKA_PUBLISH_TIMEOUT` / `KAFKA_MAX_ATTEMPTS` / `KAFKA_BREAKER_*` e `LOG_LEVEL` / `LOG_PRETTY`.

### Account Service

```bash
cd services/account-service
cp .env.example .env
docker compose up -d                  # hot reload com Air, publicado em :8082
curl http://localhost:8082/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

As migrations em `migrations/*.cql` rodam no boot contra o `CASSANDRA_KEYSPACE` (padrão `fintech_accounts`). Configuração: `SERVER_*`, `KAFKA_BROKERS` / `KAFKA_GROUP_ID` / `KAFKA_*_TIMEOUT` / `KAFKA_MAX_ATTEMPTS`, `CONSUMER_RETRY_BACKOFF` / `CONSUMER_DRAIN_TIMEOUT`, `CASSANDRA_HOSTS` / `CASSANDRA_KEYSPACE` / `CASSANDRA_CONSISTENCY` / `CASSANDRA_*_TIMEOUT` / `CASSANDRA_MIGRATIONS_PATH`, `LOG_LEVEL` / `LOG_PRETTY`.

Comandos que ele trata (tópico `account.commands`) e os eventos com que ele responde (tópico `account.events`):

| Comando | Resultado | Dead-letter (`account.dlq`) quando |
|---|---|---|
| `account.create` | `account.created` | dados inválidos, colisão de número após 5 tentativas |
| `account.update` | `account.updated` | conta desconhecida, conta fechada, update vazio |
| `account.delete` | `account.deleted` | conta desconhecida, saldo diferente de zero |
| `account.credit` | `account.credited` ou `account.credit_rejected` (`account_not_active`, `account_not_found`) | valor ou moeda inválidos |
| `account.debit` | `account.debited` ou `account.debit_rejected` (`insufficient_funds`, `account_not_active`, `account_not_found`) | valor ou moeda inválidos |

Cada comando é aplicado no máximo uma vez (`processed_events`, TTL de 7 dias); falhas transitórias são retentadas com `CONSUMER_RETRY_BACKOFF` e depois enviadas para a dead-letter como `account.command_failed`, com `retries` contando as tentativas de despacho. Um write timeout ou unavailable do Cassandra, um timeout do lado do cliente, ou uma escrita cujo contexto foi cancelado ou expirou vai direto para a dead-letter como `ambiguous_write`, porque a escrita pode ou não ter sido aplicada e retentar poderia aplicá-la duas vezes. Como o id do evento é marcado antes do despacho, um comando da dead-letter reenviado tal como está é ignorado como duplicado: replays precisam de um novo id de evento. No shutdown o consumer termina a mensagem em andamento (até `CONSUMER_DRAIN_TIMEOUT`) antes de fazer o commit, e um consumer que para por erro é reiniciado com `CONSUMER_RETRY_BACKOFF` enquanto a API de leitura continua atendendo.

### Transaction Service

```bash
cd services/transaction-service
cp .env.example .env
docker compose up -d                  # hot reload com Air, publicado em :8083
curl http://localhost:8083/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

As migrations em `migrations/*.cql` rodam no boot contra o `CASSANDRA_KEYSPACE` (padrão `fintech_transactions`). A configuração reaproveita os nomes de variável do account service, com `SERVER_PORT` padrão `8083`, `KAFKA_GROUP_ID` padrão `transaction-service` e `CASSANDRA_KEYSPACE` padrão `fintech_transactions`.

Consome `transaction.commands` e as respostas do account service em `account.events`:

| Tópico | Grupo | Trata |
|---|---|---|
| `transaction.commands` | `KAFKA_GROUP_ID` | `transaction.create` (depósito/saque), `transaction.transfer` |
| `account.events` | `KAFKA_GROUP_ID-replies` | `account.credited`, `account.debited`, `account.debit_rejected`, `account.credit_rejected` |

Uma transação é registrada como `pending` com sua chave de idempotência reservada primeiro — uma chave repetida é um no-op — e então conduzida como uma saga sobre `account.commands`: um depósito ou saque pede um único crédito ou débito e se resolve como `completed` ou `failed` (`insufficient_funds`, `account_not_active`, `account_not_found`); uma transferência debita a origem (`pending` → `debited`), credita a contraparte (`debited` → `completed`), e compensa com um crédito de volta para a origem se esse crédito for rejeitado (`reversing` → `reversed`, ou `reversal_failed` mais uma entrada em `transaction.dlq` se a própria compensação for rejeitada). Cada etapa carrega sua própria chave de idempotência (`<id da transação>:debit`, `:credit` ou `:reversal`) e cada mudança de status é uma lightweight transaction do Cassandra condicionada ao status esperado, então uma resposta duplicada ou atrasada é ignorada. Os resultados são publicados em `transaction.events` (`transaction.created`, `transaction.completed`/`transaction.failed`, `transaction.transfer_completed`/`transaction.transfer_failed`); falhas transitórias são retentadas com `CONSUMER_RETRY_BACKOFF` e depois enviadas para a dead-letter em `transaction.dlq` como `transaction.command_failed`.

**Limitações conhecidas.** Uma transação fica em `pending`, `debited` ou `reversing` se a resposta do account service nunca chegar — por exemplo após uma queda entre a marcação da resposta como processada e a mudança de status, ou quando o lado das contas envia o comando para a dead-letter como `ambiguous_write`. Transações não terminais podem ser encontradas por `GET /accounts/{account_id}/transactions`. Como no account service, um evento da dead-letter reenviado tal como está é ignorado como duplicado, então um replay precisa de um novo id de evento. No primeiro deploy o grupo `-replies` lê `account.events` desde o início; isso é inofensivo e acontece uma única vez, já que uma resposta que não corresponde a uma etapa conhecida é ignorada. Um sweeper de reconciliação para transações presas está planejado para o Sprint 6 (veja o [roadmap](#roadmap)).

### Payment Service

```bash
cd services/payment-service
cp .env.example .env
docker compose up -d                  # hot reload com Air, publicado em :8084
curl http://localhost:8084/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

As migrations em `migrations/*.cql` rodam no boot contra o `CASSANDRA_KEYSPACE` (padrão `fintech_payments`). A configuração reaproveita os nomes de variável do transaction service, com `SERVER_PORT` padrão `8084`, `KAFKA_GROUP_ID` padrão `payment-service` e `CASSANDRA_KEYSPACE` padrão `fintech_payments`, mais `PAYMENT_WEBHOOK_SECRET` (obrigatória, com pelo menos 16 caracteres), `PAYMENT_WEBHOOK_URL` (padrão `http://localhost:8084/webhooks/gateway`), `PAYMENT_WEBHOOK_TOLERANCE` (padrão `5m`) e `PAYMENT_SETTLEMENT_DELAY` (padrão `2s`). O valor `dev-webhook-secret` em `docker-compose.yml` e `.env.example` serve apenas para desenvolvimento local; em qualquer outro ambiente, substitua-o por um segredo aleatório com pelo menos 16 caracteres.

Consome `payment.commands` e as respostas do account service em `account.events`:

| Tópico | Grupo | Trata |
|---|---|---|
| `payment.commands` | `KAFKA_GROUP_ID` | `payment.process`, `payment.submit`, `payment.settle` |
| `account.events` | `KAFKA_GROUP_ID-replies` | `account.credited`, `account.debited`, `account.debit_rejected`, `account.credit_rejected` |

Um pagamento é registrado como `pending` com sua chave de idempotência reservada primeiro, depois debita a conta (`pending` → `debited`) e é submetido ao provedor sandbox: PIX se resolve na hora (`completed`), TED e boleto são vinculados a um id externo e aguardam um webhook (`debited` → `submitted` → `completed`). Uma rejeição na etapa de débito falha o pagamento diretamente (`failed`, `insufficient_funds`, `account_not_active` ou `account_not_found`); uma rejeição na submissão ou na liquidação em vez disso estorna o débito (`refunding`) e chega a `refunded`, ou a `refund_failed` mais uma entrada em `payment.dlq` se o próprio estorno for rejeitado. Cada mudança de status é uma lightweight transaction do Cassandra condicionada ao status esperado, e os resultados são publicados em `payment.events` (`payment.created`, `payment.processed`, `payment.completed`, `payment.failed`).

O provedor sandbox informa a liquidação através de um webhook assinado: `POST /webhooks/gateway` com corpo `{external_id, status: settled|rejected, reason}`, headers `X-Timestamp` (unix seconds) e `X-Signature` (HMAC-SHA256 em hex de `timestamp + "." + body`), aceito dentro de `PAYMENT_WEBHOOK_TOLERANCE` do horário atual. Respostas: `202` com o id do comando, `401 INVALID_SIGNATURE`, `413 PAYLOAD_TOO_LARGE` (corpo acima de 64 KiB), `400 INVALID_JSON`, `422 VALIDATION_ERROR`, `503 PUBLISH_FAILED`.

**Valores do sandbox.**

| Entrada | Resultado |
|---|---|
| Chave PIX terminada em `@reject.test` | rejeitado, `pix_key_not_found` |
| `bank_code` `999` no TED | rejeitado, `invalid_destination` |
| Código de boleto começando com `999` | rejeitado, `boleto_not_found` |
| Qualquer outro valor | se resolve; TED e boleto se resolvem depois de `PAYMENT_SETTLEMENT_DELAY` |

**Limitações conhecidas.** Um pagamento fica em `pending` se o resultado do débito nunca chegar, em `debited` se o provedor continuar falhando depois das tentativas e o comando for para a dead-letter, em `submitted` se a liquidação nunca chegar (por exemplo, quando o sandbox perde um callback agendado porque o serviço reiniciou antes de `PAYMENT_SETTLEMENT_DELAY` passar) até que uma reconciliação o reenvie, e em `refunding` se o resultado do crédito de estorno nunca chegar. Pagamentos não terminais podem ser encontrados por `GET /accounts/{account_id}/payments`. Uma liquidação que chega antes de o id externo ser vinculado, ou enquanto a submissão ainda está sendo registrada, é retentada e enviada para a dead-letter como `conflict` se nunca for aplicada. Como nos outros serviços, um evento da dead-letter reenviado tal como está é ignorado como duplicado, então um replay precisa de um novo id de evento. No primeiro deploy o serviço lê `payment.commands` e `account.events` desde o início. Um sweeper de reconciliação está planejado para o Sprint 6 (veja o [roadmap](#roadmap)); antes que ele possa reenviar créditos e débitos com segurança, o account service precisa garantir as chaves de idempotência nessas operações.

### Notification Service

```bash
cd services/notification-service
cp .env.example .env
docker compose up -d                  # hot reload com Air, publicado em :8085
curl http://localhost:8085/health     # {"success":true,"data":{"status":"healthy","redis":"up"}}
```

Configuração: `SERVER_*`, `KAFKA_BROKERS` / `KAFKA_GROUP_ID` (padrão `notification-service`) / `KAFKA_*_TIMEOUT` / `KAFKA_MAX_ATTEMPTS`, `CONSUMER_RETRY_BACKOFF` / `CONSUMER_DRAIN_TIMEOUT`, `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB`, `ACCOUNT_SERVICE_URL` / `ACCOUNT_DIRECTORY_TTL` (padrão `5m`) / `ACCOUNT_DIRECTORY_TIMEOUT` (padrão `3s`), `SMTP_ADDR` / `SMTP_FROM` (padrão `no-reply@fintech.local`) / `SMTP_TIMEOUT` (padrão `10s`), `NOTIFICATION_HISTORY_SIZE` (padrão `100`), `NOTIFICATION_MAX_EVENT_AGE` (padrão `1h`, `0` desativa), `LOG_LEVEL` / `LOG_PRETTY`.

O serviço roda dois tipos de consumer: o roteamento transforma eventos de resultado dos domínios em comandos de entrega, e a entrega os envia e registra o histórico.

| Tópico | Grupo | Trata |
|---|---|---|
| `account.events` | `KAFKA_GROUP_ID-accounts` | roteia `account.created` |
| `transaction.events` | `KAFKA_GROUP_ID-transactions` | roteia `transaction.completed`, `transaction.failed`, `transaction.transfer_completed`, `transaction.transfer_failed` |
| `payment.events` | `KAFKA_GROUP_ID-payments` | roteia `payment.completed`, `payment.failed` |
| `notification.events` | `KAFKA_GROUP_ID` | entrega `notification.email`, `notification.sms`, `notification.push`, com chave por id do usuário |

O roteamento renderiza um entre oito templates em português e transforma cada evento de resultado nos canais abaixo, buscando o titular da conta no account service e ignorando contas que ele não conhece:

| Evento | Destinatário | Canais |
|---|---|---|
| `account.created` | titular da conta | e-mail (boas-vindas) |
| `transaction.completed` | titular da conta | push |
| `transaction.failed` | titular da conta | push + e-mail |
| `transaction.transfer_completed` | remetente e contraparte | push para os dois |
| `transaction.transfer_failed` | remetente | push + e-mail |
| `payment.completed` | titular da conta | push + e-mail |
| `payment.failed` | titular da conta | push + e-mail, mais SMS quando `status` é `refund_failed` e o titular tem telefone |

Falhas transitórias de roteamento e de entrega são retentadas com `CONSUMER_RETRY_BACKOFF` e depois enviadas para a dead-letter como `notification.command_failed` em `notification.dlq`. Os contatos vêm do endpoint interno `GET /accounts/{id}/owner` do account service (`{account_id, user_id, name, email, phone?}`, `404 ACCOUNT_NOT_FOUND`) — não exposto pelo gateway — cacheados em memória por `ACCOUNT_DIRECTORY_TTL`.

A entrega envia e-mail por SMTP — Mailpit em desenvolvimento, com interface web em http://localhost:8025 — e SMS e push por provedores sandbox que apenas registram a mensagem em log; os valores nos templates aparecem como `R$ 1.234,56`. Um destinatário de e-mail malformado vai para a dead-letter em vez de ser retentado, e cada entrega SMTP — conexão e conversa inteira — é limitada por `SMTP_TIMEOUT`. Cada evento é processado no máximo uma vez mesmo com quedas, porque a marca de processado é gravada antes da entrega: uma queda no meio da entrega pode perder uma notificação, enquanto uma nova tentativa no mesmo processo após um erro ambíguo do provedor, ou uma marca de deduplicação perdida (expulsa pelo Redis), pode repeti-la.

O Redis mantém uma marca `notification:processed:<id do evento>` por 7 dias para deduplicar entregas e uma lista `notification:history:<id do usuário>`, mais recentes primeiro, limitada a `NOTIFICATION_HISTORY_SIZE` e expirando 90 dias depois da notificação mais recente; o compose raiz roda o Redis com `allkeys-lru` e 128 MB, então a deduplicação é best-effort quando a pressão de memória expulsa chaves antigas. API de histórico: `GET /users/{user_id}/notifications?limit=` (padrão 20, 1–100, `422` caso contrário) → `[{id, channel, recipient, subject?, body, source_event_id?, sent_at}]`, com o destinatário mascarado (`a***@example.com`, `+55*******7766`; push mantém o id do usuário).

Os consumers de roteamento leem a partir do offset retido mais antigo e ignoram eventos de origem mais velhos que `NOTIFICATION_MAX_EVENT_AGE` (padrão `1h`), para que um grupo de consumer novo ou uma indisponibilidade longa não inunde os clientes com notificações velhas; o grupo de entrega em `notification.events` também começa do offset mais antigo, então nenhum comando de entrega já publicado é perdido num redeploy; comandos já roteados antes de uma indisponibilidade não têm verificação de idade e ainda são entregues com atraso. O TTL de 7 dias da marca de processado precisa continuar maior que a retenção de 24 h do log do Kafka, para que um evento reprocessado ainda seja reconhecido como já processado.

**Limitações conhecidas.** Ainda não há autenticação: o endpoint de histórico devolve o corpo das mensagens para qualquer id de usuário (com os destinatários mascarados) e precisa ser restrito ao titular da conta quando houver autenticação. O endpoint interno `GET /accounts/{id}/owner` do account service não tem autenticação e não é repassado pelo gateway; sua porta é publicada apenas para desenvolvimento.

#### Endpoints de comando

Toda escrita é aceita de forma assíncrona: o gateway valida o corpo, publica um comando no Kafka e responde `202` com o id do comando e o trace id (`X-Request-ID`).

| Método | Caminho | Tópico | Tipo de evento |
|---|---|---|---|
| `POST` | `/api/v1/accounts` | `account.commands` | `account.create` |
| `PATCH` | `/api/v1/accounts/{id}` | `account.commands` | `account.update` |
| `DELETE` | `/api/v1/accounts/{id}` | `account.commands` | `account.delete` |
| `POST` | `/api/v1/transactions` | `transaction.commands` | `transaction.create` |
| `POST` | `/api/v1/transfers` | `transaction.commands` | `transaction.transfer` |
| `POST` | `/api/v1/payments` | `payment.commands` | `payment.process` |

```bash
curl -s -X POST localhost:8081/api/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01","account_type":"checking","name":"Ana Souza","email":"ana@example.com","document":"52998224725"}'
# {"success":true,"data":{"command_id":"…","trace_id":"…"}}
```

Erros: `400 INVALID_JSON`, `413 PAYLOAD_TOO_LARGE` (corpo acima de 1 MiB), `422 VALIDATION_ERROR` (com `details` por campo), `422 EMPTY_UPDATE` (PATCH sem campos), `429 RATE_LIMIT_EXCEEDED`, `503 PUBLISH_FAILED` quando o broker está inacessível ou o circuito está aberto.

**Fluxo da transação**: `POST /transactions` (depósito/saque) e `POST /transfers` são aceitos com `202`; o transaction service registra a transação como `pending`, pede ao account service para debitar/creditar, e a resolve como `completed` ou `failed` (`insufficient_funds`, `account_not_active`, `account_not_found`); uma transferência cujo crédito é rejeitado depois do débito é compensada (`reversed`, ou `reversal_failed` + `transaction.dlq` quando a própria compensação é rejeitada). O mesmo `idempotency_key` nunca cria uma segunda transação.

**Fluxo do pagamento**: `POST /payments` exige um objeto `ted` (`bank_code`, `branch`, `account`, `document`) para pagamentos por TED — rejeitado em qualquer outro método (`422 ted: excluded`) — e valida os dígitos verificadores do `boleto_code` (`422 boleto_code: boleto`); o payment service debita a conta, submete a um provedor sandbox, e se resolve como `completed` (PIX na hora, TED e boleto depois de um webhook assinado) ou estorna o débito e se resolve como `failed`. O mesmo `idempotency_key` nunca cria um segundo pagamento.

#### Endpoints de leitura

As leituras são repassadas por proxy ao account service via `ACCOUNT_SERVICE_URL`, ao transaction service via `TRANSACTION_SERVICE_URL`, ao payment service via `PAYMENT_SERVICE_URL` e ao notification service via `NOTIFICATION_SERVICE_URL` (padrão `http://localhost:8085`); `502 UPSTREAM_UNAVAILABLE` quando o upstream está fora do ar.

| Método | Caminho | Upstream |
|---|---|---|
| `GET` | `/api/v1/accounts/{id}` | `GET /accounts/{id}` → `200` conta (`balance` em BRL), `404 ACCOUNT_NOT_FOUND`, `422` |
| `GET` | `/api/v1/users/{user_id}/accounts` | `GET /users/{user_id}/accounts` → `200` lista |
| `GET` | `/api/v1/transactions/{id}` | `GET /transactions/{id}` → `200` transação (`status` pending/debited/completed/failed/reversing/reversed/reversal_failed, saldos após cada etapa), `404 TRANSACTION_NOT_FOUND`, `422` |
| `GET` | `/api/v1/accounts/{account_id}/transactions` | `GET /accounts/{account_id}/transactions?limit=50` → `200` mais recentes primeiro (`limit` 1–200) |
| `GET` | `/api/v1/payments/{id}` | `GET /payments/{id}` → `200` pagamento (`status` pending/debited/submitted/completed/failed/refunding/refunded/refund_failed), `404 PAYMENT_NOT_FOUND`, `422` |
| `GET` | `/api/v1/accounts/{account_id}/payments` | `GET /accounts/{account_id}/payments?limit=50` → `200` mais recentes primeiro (`limit` 1–200) |
| `GET` | `/api/v1/users/{user_id}/notifications` | `GET /users/{user_id}/notifications?limit=20` → `200` mais recentes primeiro (`limit` 1–100), `422` |

## Desenvolvimento

```bash
# pacotes compartilhados
cd pkg && make test && make lint            # go test ./... · checagem de gofmt

# api gateway
cd services/api-gateway
make test                                   # unit + feature, cobertura de ./internal/...
make test-coverage                          # gera coverage.html
make test-integration                       # precisa de KAFKA_BROKERS apontando para um broker

# account service
cd services/account-service
make test                                   # unit + feature, cobertura de ./internal/app/...
make test-coverage                          # gera coverage.html
make test-integration                       # precisa de KAFKA_BROKERS e CASSANDRA_HOSTS

# transaction service
cd services/transaction-service
make test                                   # unit + feature, cobertura de ./internal/app/...
make test-coverage                          # gera coverage.html
make test-integration                       # precisa de KAFKA_BROKERS e CASSANDRA_HOSTS

# payment service
cd services/payment-service
make test                                   # unit + feature, cobertura de ./internal/app/...
make test-coverage                          # gera coverage.html
make test-integration                       # precisa de KAFKA_BROKERS e CASSANDRA_HOSTS

# notification service
cd services/notification-service
make test                                   # unit + feature, cobertura de ./internal/app/...
make test-coverage                          # gera coverage.html
make test-integration                       # precisa de KAFKA_BROKERS, REDIS_ADDR e SMTP_ADDR / MAILPIT_URL apontando para o Mailpit
```

O CI (`.github/workflows/ci.yml`) roda a cada push e pull request em seis jobs — `pkg`, `api-gateway` (com um container de serviço Kafka), `account-service`, `transaction-service` e `payment-service` (com containers de serviço Kafka e Cassandra) e `notification-service` (com containers de serviço Kafka, Redis e Mailpit) — checagem de `gofmt` e as suítes de `pkg`, `api-gateway`, `account-service`, `transaction-service`, `payment-service` e `notification-service`, falhando o build se a cobertura cair abaixo de 100 % (de `internal/app` para o account service, o transaction service, o payment service e o notification service).

## Estrutura do projeto

```
fintech-bank-platform/
├── docker-compose.yml         Kafka · Cassandra · Redis (+ profile ui)
├── .env.example               todas as variáveis que a plataforma lê
├── .github/workflows/ci.yml   gofmt + testes + gate de cobertura
├── pkg/                       módulo Go compartilhado
│   ├── logger/  errors/  response/  validation/  events/
│   ├── env/  middleware/  messaging/
│   ├── domain/  cassandra/  processor/
│   ├── Makefile · Dockerfile · docker-compose.yml
│   └── README.md
└── services/
    ├── api-gateway/
    │   ├── cmd/main.go                    ponto de entrada
    │   ├── internal/
    │   │   ├── config/                    env → Config tipada
    │   │   ├── contracts/                 interfaces de config, contexto e http
    │   │   ├── app/handlers/              endpoints de comando
    │   │   └── infrastructure/
    │   │       ├── http/                  server, router, handlers, middleware/
    │   │       └── messaging/             producer kafka, circuit breaker
    │   ├── tests/  (unit/ · feature/ · integration/)     helpers TestCase no estilo testify
    │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
    │   └── .env.example
    ├── account-service/
    │   ├── cmd/main.go                    ponto de entrada
    │   ├── migrations/                    arquivos .cql numerados, aplicados no boot
    │   ├── internal/
    │   │   ├── config/                    env → Config tipada
    │   │   ├── contracts/                 interfaces de config, messaging e repositórios
    │   │   ├── app/
    │   │   │   ├── models/                tipos de domínio
    │   │   │   ├── services/              casos de uso de contas e clientes
    │   │   │   └── handlers/              dispatcher de comandos, DLQ, endpoints de leitura
    │   │   └── infrastructure/
    │   │       ├── database/              repositórios Cassandra e migrations
    │   │       └── http/                  server, router, health, handlers de leitura
    │   ├── tests/  (unit/ · feature/ · integration/)
    │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
    │   └── .env.example
    ├── transaction-service/
    │   ├── cmd/main.go                    ponto de entrada
    │   ├── migrations/                    arquivos .cql numerados, aplicados no boot
    │   ├── internal/
    │   │   ├── config/                    env → Config tipada
    │   │   ├── contracts/                 interfaces de config, messaging e repositórios
    │   │   ├── app/
    │   │   │   ├── models/                tipos de domínio
    │   │   │   ├── services/              casos de uso de transações e transições da saga
    │   │   │   └── handlers/              dispatchers de comando e resposta, endpoints de leitura
    │   │   └── infrastructure/
    │   │       ├── database/              repositórios Cassandra e migrations
    │   │       └── http/                  server, router, health, handlers de leitura
    │   ├── tests/  (unit/ · feature/ · integration/)
    │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
    │   └── .env.example
    ├── payment-service/
    │   ├── cmd/main.go                    ponto de entrada
    │   ├── migrations/                    arquivos .cql numerados, aplicados no boot
    │   ├── internal/
    │   │   ├── config/                    env → Config tipada
    │   │   ├── contracts/                 interfaces de config, messaging e repositórios
    │   │   ├── app/
    │   │   │   ├── models/                tipos de domínio
    │   │   │   ├── services/              casos de uso de pagamentos e transições da saga
    │   │   │   └── handlers/              dispatchers de comando, resposta e webhook, endpoints de leitura
    │   │   └── infrastructure/
    │   │       ├── database/              repositórios Cassandra e migrations
    │   │       ├── gateway/               simulador do provedor sandbox
    │   │       └── http/                  server, router, health, handlers de leitura
    │   ├── tests/  (unit/ · feature/ · integration/)
    │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
    │   └── .env.example
    └── notification-service/
        ├── cmd/main.go                    ponto de entrada
        ├── internal/
        │   ├── config/                    env → Config tipada
        │   ├── contracts/                 interfaces de config, messaging, senders e storage
        │   ├── app/
        │   │   ├── models/                tipos de domínio
        │   │   ├── services/              roteamento, renderização e entrega, templates/
        │   │   └── handlers/              endpoint de leitura do histórico
        │   └── infrastructure/
        │       ├── directory/             cliente do endpoint de titular do account service, cache em memória
        │       ├── senders/               senders de SMTP e sandbox de SMS/push
        │       ├── storage/               cliente Redis, store de idempotência e histórico
        │       └── http/                  server, router, health, handler do histórico
        ├── tests/  (unit/ · feature/ · integration/)
        ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
        └── .env.example
```

Cada serviço futuro segue o mesmo layout: `cmd/`, `internal/{config,contracts,infrastructure,app}` e `tests/`, com `migrations/` (CQL) para os que persistem no Cassandra.

## Roadmap

- [x] **Sprint 0** — infraestrutura em Docker Compose (Kafka KRaft, Cassandra, Redis, UIs de debug)
- [x] **Pacotes compartilhados** — logger, errors, response, validation, events, com CI e 100 % de cobertura
- [x] **Sprint 1 — API Gateway** — esqueleto HTTP, middlewares, config, producer Kafka com circuit breaker e endpoints de comando
- [x] **Sprint 2 — Account Service** — clientes e contas no Cassandra, saldo com compare-and-set, eventos de resultado, API de leitura repassada por proxy pelo gateway
- [x] **Sprint 3 — Transaction Service** — depósitos, saques e transferências como sagas sobre o account service, chaves de idempotência, compensação, API de leitura repassada por proxy pelo gateway
- [x] **Sprint 4 — Payment Service** — PIX, TED e boleto como sagas sobre o account service, provedor sandbox com webhooks assinados, estornos, API de leitura repassada por proxy pelo gateway
- [x] **Sprint 5 — Notification Service** — e-mail, SMS e push a partir dos eventos de resultado, templates, idempotência e histórico apoiados em Redis, repassado por proxy pelo gateway
- [ ] **Sprint 6** — testes end-to-end, de carga e um sweeper de reconciliação para transações e pagamentos presos
- [ ] **Sprint 7** — observabilidade (Prometheus, Jaeger) e docs

## Licença

[MIT](LICENSE) © [Gabriel Silva](https://github.com/GabeSilvaDev)
