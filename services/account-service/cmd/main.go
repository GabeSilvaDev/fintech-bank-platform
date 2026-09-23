package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/config"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/go-chi/chi/v5"
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

	service := services.NewAccountService(
		database.NewAccountRepository(session),
		database.NewCustomerRepository(session),
		database.NewBalanceOperationRepository(session),
		services.SystemClock{},
		services.RandomNumber,
	)

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	})
	proc := processor.NewProcessor(handlers.NewDispatcher(service), database.NewProcessedEventStore(session), producer, processor.Config{
		Source:          "account-service",
		FailedEventType: events.EventTypes.AccountCommandFailed,
		DLQTopic:        events.Topics.AccountDLQ,
		Backoff:         cfg.Consumer.RetryBackoff,
	}, log)
	newConsumer := func() *messaging.Consumer {
		return messaging.NewConsumer(messaging.ConsumerConfig{
			Brokers:      cfg.Kafka.Brokers,
			GroupID:      cfg.Kafka.GroupID,
			Topic:        events.Topics.AccountCommands,
			DrainTimeout: cfg.Consumer.DrainTimeout,
		})
	}
	handle := func(ctx context.Context, msg kafka.Message) error {
		return proc.Process(ctx, msg.Key, msg.Value)
	}

	router := chi.NewRouter()
	http.SetupRouter(router, http.Dependencies{
		Reads:  handlers.NewReadHandler(service),
		Ping:   database.Ping(session),
		Logger: log,
	})
	server := http.NewServer(cfg.Server, router)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		log.Info().Str("topic", events.Topics.AccountCommands).Str("group", cfg.Kafka.GroupID).Msg("Consumer starting")
		messaging.RunWithRestart(groupCtx, newConsumer, handle, cfg.Consumer.RetryBackoff, func(err error) {
			log.Error().Err(err).Msg("consumer stopped, restarting")
		})
		return nil
	})
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
