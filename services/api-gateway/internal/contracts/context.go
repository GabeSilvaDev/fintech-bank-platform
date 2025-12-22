package contracts

type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
)

const RequestIDHeader = "X-Request-ID"
