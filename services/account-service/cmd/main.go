package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/config"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/pkg/retry"
	"github.com/fintech-bank-platform/pkg/tracing"
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

	shutdownTracing, err := tracing.Init(context.Background(), tracing.Config{
		Service:     "account-service",
		Endpoint:    cfg.Observability.OTLPEndpoint,
		SampleRatio: cfg.Observability.SampleRatio,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize tracing")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown tracing")
		}
	}()

	var m *metrics.Metrics
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("account-service")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	session, applied, err := connect(ctx, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Cassandra connection failed")
	}
	defer session.Close()
	log.Info().Ints("versions", applied).Msg("Migrations applied")

	service := services.NewAccountService(
		database.NewAccountRepository(session),
		database.NewCustomerRepository(session),
		database.NewBalanceOperationRepository(session),
		services.SystemClock{},
		services.RandomNumber,
	)

	identities, err := services.NewIdentityService(database.NewIdentityRepository(session), services.NewBcryptHasher(services.BcryptCost), services.SystemClock{})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize identity service")
	}
	identities.WithHashConcurrency(cfg.Identity.HashConcurrency, services.HashWait)
	sessions := services.NewSessionService(database.NewRefreshTokenRepository(session), services.SystemClock{}, cfg.Session.RefreshTokenTTL)

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	}).WithMetrics(m)
	proc := processor.NewProcessor(handlers.NewDispatcher(service), database.NewProcessedEventStore(session), producer, processor.Config{
		Source:          "account-service",
		FailedEventType: events.EventTypes.AccountCommandFailed,
		DLQTopic:        events.Topics.AccountDLQ,
		Backoff:         cfg.Consumer.RetryBackoff,
		Metrics:         m,
	}, log)
	newConsumer := func() *messaging.Consumer {
		return messaging.NewConsumer(messaging.ConsumerConfig{
			Brokers:      cfg.Kafka.Brokers,
			GroupID:      cfg.Kafka.GroupID,
			Topic:        events.Topics.AccountCommands,
			DrainTimeout: cfg.Consumer.DrainTimeout,
			Metrics:      m,
		})
	}
	handle := func(ctx context.Context, msg kafka.Message) error {
		return proc.Process(ctx, msg.Key, msg.Value)
	}

	router := chi.NewRouter()
	http.SetupRouter(router, http.Dependencies{
		Reads:      handlers.NewReadHandler(service),
		Identities: handlers.NewIdentityHandler(identities),
		Sessions:   handlers.NewSessionHandler(sessions),
		Ping:       database.Ping(session),
		Logger:     log,
		Metrics:    m,
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
