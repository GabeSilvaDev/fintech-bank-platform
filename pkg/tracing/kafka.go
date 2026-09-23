package tracing

import (
	"context"
	"slices"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
)

type headerCarrier struct {
	headers []kafka.Header
}

func (c *headerCarrier) Get(key string) string {
	for _, header := range c.headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

func (c *headerCarrier) Set(key, value string) {
	for i, header := range c.headers {
		if header.Key == key {
			c.headers[i].Value = []byte(value)
			return
		}
	}

	c.headers = append(c.headers, kafka.Header{Key: key, Value: []byte(value)})
}

func (c *headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for _, header := range c.headers {
		keys = append(keys, header.Key)
	}

	return keys
}

func Inject(ctx context.Context, headers []kafka.Header) []kafka.Header {
	carrier := &headerCarrier{headers: slices.Clone(headers)}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	return carrier.headers
}

func Extract(ctx context.Context, headers []kafka.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, &headerCarrier{headers: headers})
}
