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

type CassandraConfig struct {
	Hosts          []string
	Keyspace       string
	Consistency    string
	Timeout        time.Duration
	ConnectTimeout time.Duration
	MigrationsPath string
}

type ConsumerConfig struct {
	RetryBackoff []time.Duration
	DrainTimeout time.Duration
}

type LogConfig struct {
	Level  string
	Pretty bool
}

type StartupConfig struct {
	Attempts int
	Delay    time.Duration
}
