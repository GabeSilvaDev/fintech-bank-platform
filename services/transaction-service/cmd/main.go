package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/internal/config"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/http"
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

	bootstrap, err := database.NewSession(cfg.Cassandra, "")
	if err != nil {
		log.Fatal().Err(err).Msg("Cassandra connection failed")
	}
	applied, err := database.NewMigrator(bootstrap, cfg.Cassandra.Keyspace, os.DirFS(cfg.Cassandra.MigrationsPath)).Up(ctx)
	bootstrap.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Migrations failed")
	}
	log.Info().Ints("versions", applied).Msg("Migrations applied")

	session, err := database.NewSession(cfg.Cassandra, cfg.Cassandra.Keyspace)
	if err != nil {
		log.Fatal().Err(err).Msg("Cassandra connection failed")
	}
	defer session.Close()

	service := services.NewTransactionService(database.NewTransactionRepository(session), services.SystemClock{}, uuid.New)

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	})
	store := database.NewProcessedEventStore(session)
	processorCfg := processor.Config{
		Source:          "transaction-service",
		FailedEventType: events.EventTypes.TransactionCommandFailed,
		DLQTopic:        events.Topics.TransactionDLQ,
		Backoff:         cfg.Consumer.RetryBackoff,
	}
	commands := processor.NewProcessor(handlers.NewCommandDispatcher(service, log), store, producer, processorCfg, log)
	replies := processor.NewProcessor(handlers.NewReplyDispatcher(service), store, producer, processorCfg, log)

	router := chi.NewRouter()
	http.SetupRouter(router, http.Dependencies{
		Reads:  handlers.NewReadHandler(service),
		Ping:   database.Ping(session),
		Logger: log,
	})
	server := http.NewServer(cfg.Server, router)

	group, groupCtx := errgroup.WithContext(ctx)
	runConsumer(groupCtx, group, log, cfg, events.Topics.TransactionCommands, cfg.Kafka.GroupID, commands)
	runConsumer(groupCtx, group, log, cfg, events.Topics.AccountEvents, cfg.Kafka.GroupID+"-replies", replies)
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
