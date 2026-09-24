package httpmethod

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKnownAcceptsStandardMethods(t *testing.T) {
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace,
	} {
		assert.True(t, Known(method), method)
	}
}

func TestKnownRejectsOtherMethods(t *testing.T) {
	for _, method := range []string{"FOO", "get", "PROPFIND", ""} {
		assert.False(t, Known(method), method)
	}
}
