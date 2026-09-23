package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/fintech-bank-platform/notification-service/internal/config"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/directory"
	appHttp "github.com/fintech-bank-platform/notification-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/senders"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/storage"
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
		Service:     "notification-service",
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
		m = metrics.New("notification-service")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := storage.NewClient(cfg.Redis)
	attempt := 0
	err = retry.Do(ctx, cfg.Startup.Attempts, cfg.Startup.Delay, func() error {
		attempt++
		if err := storage.Ping(client)(ctx); err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("redis not ready, retrying")
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Redis connection failed")
	}

	accountDirectory := directory.NewClient(cfg.Directory.URL, cfg.Directory.Timeout, cfg.Directory.TTL)
	renderer, err := services.NewRenderer(services.Templates())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load templates")
	}
	router := services.NewRouter(accountDirectory, renderer, services.SystemClock{}, cfg.Consumer.MaxEventAge, log)

	senderMap := map[models.Channel]contracts.Sender{
		models.ChannelEmail: senders.NewSMTP(cfg.SMTP.Addr, cfg.SMTP.From, cfg.SMTP.Timeout),
		models.ChannelSMS:   senders.NewSandbox(models.ChannelSMS, log),
		models.ChannelPush:  senders.NewSandbox(models.ChannelPush, log),
	}
	history := storage.NewHistory(client, cfg.HistorySize)
	delivery := services.NewDelivery(senderMap, history, services.SystemClock{}, log).WithMetrics(m)

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	}).WithMetrics(m)

	store := storage.NewStore(client)
	processorCfg := processor.Config{
		Source:          "notification-service",
		FailedEventType: events.EventTypes.NotificationCommandFailed,
		DLQTopic:        events.Topics.NotificationDLQ,
		Backoff:         cfg.Consumer.RetryBackoff,
		Metrics:         m,
	}
	routingProcessor := processor.NewProcessor(router, store, producer, processorCfg, log)
	deliveryProcessor := processor.NewProcessor(delivery, store, producer, processorCfg, log)

	chiRouter := chi.NewRouter()
	appHttp.SetupRouter(chiRouter, appHttp.Dependencies{
		History: handlers.NewHistoryHandler(history),
		Ping:    storage.Ping(client),
		Logger:  log,
		Metrics: m,
	})
	server := appHttp.NewServer(cfg.Server, chiRouter)

	group, groupCtx := errgroup.WithContext(ctx)
	runConsumer(groupCtx, group, log, cfg, m, events.Topics.AccountEvents, cfg.Kafka.GroupID+"-accounts", kafka.FirstOffset, routingProcessor)
	runConsumer(groupCtx, group, log, cfg, m, events.Topics.TransactionEvents, cfg.Kafka.GroupID+"-transactions", kafka.FirstOffset, routingProcessor)
	runConsumer(groupCtx, group, log, cfg, m, events.Topics.PaymentEvents, cfg.Kafka.GroupID+"-payments", kafka.FirstOffset, routingProcessor)
	runConsumer(groupCtx, group, log, cfg, m, events.Topics.NotificationEvents, cfg.Kafka.GroupID, kafka.FirstOffset, deliveryProcessor)
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
	_ = client.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Service failed")
	}
	log.Info().Msg("Service stopped")
}

func runConsumer(ctx context.Context, group *errgroup.Group, log *logger.Logger, cfg *config.Config, m *metrics.Metrics, topic, groupID string, startOffset int64, proc *processor.Processor) {
	newConsumer := func() *messaging.Consumer {
		return messaging.NewConsumer(messaging.ConsumerConfig{
			Brokers:      cfg.Kafka.Brokers,
			GroupID:      groupID,
			Topic:        topic,
			DrainTimeout: cfg.Consumer.DrainTimeout,
			StartOffset:  startOffset,
			Metrics:      m,
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
