package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/internal/config"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/gateway"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/pkg/retry"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"golang.org/x/sync/errgroup"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		logger.NewDefault().Fatal().Err(err).Msg("Failed to load configuration")
	}

	log := logger.New(logger.Config{Level: cfg.Log.Level, Pretty: cfg.Log.Pretty})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	session, applied, err := connect(ctx, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Cassandra connection failed")
	}
	defer session.Close()
	log.Info().Ints("versions", applied).Msg("Migrations applied")

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	})

	simulator := gateway.NewSimulator(gateway.Config{
		WebhookURL: cfg.Payment.WebhookURL,
		Secret:     cfg.Payment.WebhookSecret,
		Delay:      cfg.Payment.SettlementDelay,
	}, log)
	service := services.NewPaymentService(database.NewPaymentRepository(session), simulator, services.SystemClock{}, uuid.New)
	sweeper := services.NewSweeper(service, producer, services.SystemClock{}, cfg.Sweeper, log)

	store := database.NewProcessedEventStore(session)
	processorCfg := processor.Config{
		Source:          "payment-service",
		FailedEventType: events.EventTypes.PaymentCommandFailed,
		DLQTopic:        events.Topics.PaymentDLQ,
		Backoff:         cfg.Consumer.RetryBackoff,
	}
	commands := processor.NewProcessor(handlers.NewCommandDispatcher(service, log), store, producer, processorCfg, log)
	replies := processor.NewProcessor(handlers.NewReplyDispatcher(service, log), store, producer, processorCfg, log)

	router := chi.NewRouter()
	http.SetupRouter(router, http.Dependencies{
		Reads:    handlers.NewReadHandler(service),
		Webhooks: handlers.NewWebhookHandler(producer, cfg.Payment.WebhookSecret, cfg.Payment.WebhookTolerance, services.SystemClock{}, log),
		Ping:     database.Ping(session),
		Logger:   log,
	})
	server := http.NewServer(cfg.Server, router)

	group, groupCtx := errgroup.WithContext(ctx)
	runConsumer(groupCtx, group, log, cfg, events.Topics.PaymentCommands, cfg.Kafka.GroupID, commands)
	runConsumer(groupCtx, group, log, cfg, events.Topics.AccountEvents, cfg.Kafka.GroupID+"-replies", replies)
	if cfg.Sweeper.Enabled {
		group.Go(func() error {
			sweeper.Run(groupCtx)
			return nil
		})
	}
	group.Go(func() error {
		log.Info().Str("address", cfg.Server.Address()).Msg("Server starting")
		return server.Start()
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	})

	err = group.Wait()
	_ = producer.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Service failed")
	}
	log.Info().Msg("Service stopped")
}

func runConsumer(ctx context.Context, group *errgroup.Group, log *logger.Logger, cfg *config.Config, topic, groupID string, proc *processor.Processor) {
	newConsumer := func() *messaging.Consumer {
		return messaging.NewConsumer(messaging.ConsumerConfig{
			Brokers:      cfg.Kafka.Brokers,
			GroupID:      groupID,
			Topic:        topic,
			DrainTimeout: cfg.Consumer.DrainTimeout,
		})
	}
	handle := func(ctx context.Context, msg kafka.Message) error {
		return proc.Process(ctx, msg.Key, msg.Value)
	}
	group.Go(func() error {
		log.Info().Str("topic", topic).Str("group", groupID).Msg("Consumer starting")
		messaging.RunWithRestart(ctx, newConsumer, handle, cfg.Consumer.RetryBackoff, func(err error) {
			log.Error().Err(err).Str("topic", topic).Msg("consumer stopped, restarting")
		})
		return nil
	})
}

func connect(ctx context.Context, cfg *config.Config, log *logger.Logger) (*gocql.Session, []int, error) {
	var session *gocql.Session
	var applied []int
	attempt := 0
	err := retry.Do(ctx, cfg.Startup.Attempts, cfg.Startup.Delay, func() error {
		attempt++
		bootstrap, err := database.NewSession(cfg.Cassandra, "")
		if err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("cassandra not ready, retrying")
			return err
		}
		versions, err := database.NewMigrator(bootstrap, cfg.Cassandra.Keyspace, os.DirFS(cfg.Cassandra.MigrationsPath)).Up(ctx)
		bootstrap.Close()
		if err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("cassandra not ready, retrying")
			return err
		}
		s, err := database.NewSession(cfg.Cassandra, cfg.Cassandra.Keyspace)
		if err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("cassandra not ready, retrying")
			return err
		}
		session = s
		applied = versions
		return nil
	})
	return session, applied, err
}
