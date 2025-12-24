// ═══════════════════════════════════════════════════════════════════════════
// TestResponse
// ═══════════════════════════════════════════════════════════════════════════

package tests

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ═══════════════════════════════════════════════════════════════════════════
// TestResponse struct
// ═══════════════════════════════════════════════════════════════════════════

type TestResponse struct {
	t        *testing.T
	recorder *httptest.ResponseRecorder
	body     []byte
	json     map[string]interface{}
}

func newTestResponse(t *testing.T, rec *httptest.ResponseRecorder) *TestResponse {
	body := rec.Body.Bytes()
	
	tr := &TestResponse{
		t:        t,
		recorder: rec,
		body:     body,
	}

	if len(body) > 0 {
		var jsonData map[string]interface{}
		if err := json.Unmarshal(body, &jsonData); err == nil {
			tr.json = jsonData
		}
	}

	return tr
}

// ═══════════════════════════════════════════════════════════════════════════
// Status Assertions
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) AssertStatus(status int) *TestResponse {
	assert.Equal(r.t, status, r.recorder.Code, "Expected status %d but got %d", status, r.recorder.Code)
	return r
}

func (r *TestResponse) AssertOk() *TestResponse {
	return r.AssertStatus(200)
}

func (r *TestResponse) AssertCreated() *TestResponse {
	return r.AssertStatus(201)
}

func (r *TestResponse) AssertAccepted() *TestResponse {
	return r.AssertStatus(202)
}

func (r *TestResponse) AssertNoContent() *TestResponse {
	return r.AssertStatus(204)
}

func (r *TestResponse) AssertBadRequest() *TestResponse {
	return r.AssertStatus(400)
}

func (r *TestResponse) AssertUnauthorized() *TestResponse {
	return r.AssertStatus(401)
}

func (r *TestResponse) AssertForbidden() *TestResponse {
	return r.AssertStatus(403)
}

func (r *TestResponse) AssertNotFound() *TestResponse {
	return r.AssertStatus(404)
}

func (r *TestResponse) AssertMethodNotAllowed() *TestResponse {
	return r.AssertStatus(405)
}

func (r *TestResponse) AssertUnprocessableEntity() *TestResponse {
	return r.AssertStatus(422)
}

func (r *TestResponse) AssertTooManyRequests() *TestResponse {
	return r.AssertStatus(429)
}

func (r *TestResponse) AssertServerError() *TestResponse {
	return r.AssertStatus(500)
}

func (r *TestResponse) AssertSuccessful() *TestResponse {
	assert.True(r.t, r.recorder.Code >= 200 && r.recorder.Code < 300, 
		"Expected successful status (2xx) but got %d", r.recorder.Code)
	return r
}

func (r *TestResponse) AssertRedirect() *TestResponse {
	assert.True(r.t, r.recorder.Code >= 300 && r.recorder.Code < 400,
		"Expected redirect status (3xx) but got %d", r.recorder.Code)
	return r
}

// ═══════════════════════════════════════════════════════════════════════════
// JSON Assertions
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) AssertJson(expected map[string]interface{}) *TestResponse {
	assert.NotNil(r.t, r.json, "Response is not valid JSON")
	
	for key, expectedValue := range expected {
		actualValue, exists := r.json[key]
		assert.True(r.t, exists, "JSON key '%s' not found in response", key)
		assert.Equal(r.t, expectedValue, actualValue, "JSON value mismatch for key '%s'", key)
	}
	return r
}

func (r *TestResponse) AssertJsonPath(path string, expected interface{}) *TestResponse {
	value := r.getJsonPath(path)
	assert.Equal(r.t, expected, value, "JSON path '%s' value mismatch", path)
	return r
}

func (r *TestResponse) AssertJsonHas(key string) *TestResponse {
	value := r.getJsonPath(key)
	assert.NotNil(r.t, value, "JSON key '%s' not found in response", key)
	return r
}

func (r *TestResponse) AssertJsonMissing(key string) *TestResponse {
	value := r.getJsonPath(key)
	assert.Nil(r.t, value, "JSON key '%s' should not be present in response", key)
	return r
}

func (r *TestResponse) AssertJsonCount(count int, key string) *TestResponse {
	value := r.getJsonPath(key)
	if arr, ok := value.([]interface{}); ok {
		assert.Len(r.t, arr, count, "JSON array '%s' count mismatch", key)
	} else {
		r.t.Errorf("JSON path '%s' is not an array", key)
	}
	return r
}

func (r *TestResponse) AssertJsonStructure(structure []string) *TestResponse {
	for _, key := range structure {
		r.AssertJsonHas(key)
	}
	return r
}

func (r *TestResponse) AssertExactJson(expected map[string]interface{}) *TestResponse {
	assert.Equal(r.t, expected, r.json, "JSON response does not match expected")
	return r
}

// ═══════════════════════════════════════════════════════════════════════════
// Header Assertions
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) AssertHeader(header, value string) *TestResponse {
	actual := r.recorder.Header().Get(header)
	assert.Equal(r.t, value, actual, "Header '%s' value mismatch", header)
	return r
}

func (r *TestResponse) AssertHeaderExists(header string) *TestResponse {
	actual := r.recorder.Header().Get(header)
	assert.NotEmpty(r.t, actual, "Header '%s' not found", header)
	return r
}

func (r *TestResponse) AssertHeaderMissing(header string) *TestResponse {
	actual := r.recorder.Header().Get(header)
	assert.Empty(r.t, actual, "Header '%s' should not be present", header)
	return r
}

func (r *TestResponse) AssertContentType(contentType string) *TestResponse {
	actual := r.recorder.Header().Get("Content-Type")
	assert.True(r.t, strings.Contains(actual, contentType),
		"Content-Type header does not contain '%s', got '%s'", contentType, actual)
	return r
}

// ═══════════════════════════════════════════════════════════════════════════
// Body Assertions
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) AssertSee(value string) *TestResponse {
	assert.Contains(r.t, string(r.body), value, "Response body does not contain '%s'", value)
	return r
}

func (r *TestResponse) AssertDontSee(value string) *TestResponse {
	assert.NotContains(r.t, string(r.body), value, "Response body should not contain '%s'", value)
	return r
}

func (r *TestResponse) AssertBodyEmpty() *TestResponse {
	assert.Empty(r.t, r.body, "Response body should be empty")
	return r
}

// ═══════════════════════════════════════════════════════════════════════════
// Success/Error Assertions
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) AssertSuccess() *TestResponse {
	return r.AssertJsonPath("success", true)
}

func (r *TestResponse) AssertError() *TestResponse {
	return r.AssertJsonPath("success", false)
}

func (r *TestResponse) AssertErrorCode(code string) *TestResponse {
	return r.AssertJsonPath("error.code", code)
}

func (r *TestResponse) AssertErrorMessage(message string) *TestResponse {
	return r.AssertJsonPath("error.message", message)
}

// ═══════════════════════════════════════════════════════════════════════════
// Getters
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) Status() int {
	return r.recorder.Code
}

func (r *TestResponse) Body() string {
	return string(r.body)
}

func (r *TestResponse) Json() map[string]interface{} {
	return r.json
}

func (r *TestResponse) Header(key string) string {
	return r.recorder.Header().Get(key)
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal Helpers
// ═══════════════════════════════════════════════════════════════════════════

func (r *TestResponse) getJsonPath(path string) interface{} {
	if r.json == nil {
		return nil
	}

	parts := strings.Split(path, ".")
	var current interface{} = r.json

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			var exists bool
			current, exists = v[part]
			if !exists {
				return nil
			}
		default:
			return nil
		}
	}

	return current
}

func (r *TestResponse) Dump() *TestResponse {
	r.t.Logf("Status: %d", r.recorder.Code)
	r.t.Logf("Headers: %v", r.recorder.Header())
	r.t.Logf("Body: %s", string(r.body))
	return r
}

func (r *TestResponse) DumpHeaders() *TestResponse {
	r.t.Logf("Headers: %v", r.recorder.Header())
	return r
}
