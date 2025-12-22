package contracts

import "net/http"

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

type ErrorInfo struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

type Meta struct {
	RequestID string `json:"request_id,omitempty"`
}

type Controller interface {
	JSON(w http.ResponseWriter, status int, data interface{})
	Error(w http.ResponseWriter, status int, code, message string)
}
