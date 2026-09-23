package contracts

import "time"

type ServerConfig struct {
	Host            string
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func (s ServerConfig) Address() string {
	return s.Host + ":" + s.Port
}

type KafkaConfig struct {
	Brokers        []string
	GroupID        string
	WriteTimeout   time.Duration
	BatchTimeout   time.Duration
	PublishTimeout time.Duration
	MaxAttempts    int
}

type ConsumerConfig struct {
	RetryBackoff []time.Duration
	DrainTimeout time.Duration
}

type LogConfig struct {
	Level  string
	Pretty bool
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type DirectoryConfig struct {
	URL     string
	TTL     time.Duration
	Timeout time.Duration
}

type SMTPConfig struct {
	Addr    string
	From    string
	Timeout time.Duration
}
