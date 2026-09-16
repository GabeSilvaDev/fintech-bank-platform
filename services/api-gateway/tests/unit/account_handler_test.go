package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func accountRouter(pub contracts.Publisher) http.Handler {
	h := handlers.NewAccountHandler(pub)
	r := chi.NewRouter()
	r.Post("/accounts", h.Create)
	r.Patch("/accounts/{id}", h.Update)
	r.Delete("/accounts/{id}", h.Delete)
	return r
}

func call(handler http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	return rec, tests.FromJson(rec.Body.String())
}

func errorDetails(body map[string]interface{}) map[string]interface{} {
	details, _ := body["error"].(map[string]interface{})["details"].(map[string]interface{})
	return details
}

func errorCode(body map[string]interface{}) string {
	code, _ := body["error"].(map[string]interface{})["code"].(string)
	return code
}

func validCreateAccountBody(userID string) string {
	return `{"user_id":"` + userID + `","account_type":"checking","name":"Ana Souza","email":"ana@example.com","document":"52998224725","phone":"11999887766"}`
}

func TestCreateAccountPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	userID := tests.UUID()

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", validCreateAccountBody(userID))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Len(t, pub.Published, 1)

	cmd := pub.Last()
	assert.Equal(t, events.Topics.AccountCommands, cmd.Topic)
	assert.Equal(t, userID, cmd.Key)
	assert.Equal(t, events.EventTypes.CreateAccount, cmd.Event.Type)
	assert.Equal(t, "req-1", cmd.Event.TraceID)

	payload := cmd.Event.Payload.(events.CreateAccountPayload)
	assert.Equal(t, userID, payload.UserID)
	assert.Equal(t, "checking", payload.AccountType)
	assert.Equal(t, "Ana Souza", payload.Name)
	assert.Equal(t, "ana@example.com", payload.Email)
	assert.Equal(t, "52998224725", payload.Document)
	assert.Equal(t, "11999887766", payload.Phone)

	data := body["data"].(map[string]interface{})
	assert.Equal(t, cmd.Event.ID, data["command_id"])
	assert.Equal(t, "req-1", data["trace_id"])
}

func TestCreateAccountNormalizesDocumentAndPhone(t *testing.T) {
	pub := &tests.FakePublisher{}
	userID := tests.UUID()

	rec, _ := call(accountRouter(pub), http.MethodPost, "/accounts", `{"user_id":"`+userID+`","account_type":"savings","name":"Ana Souza","email":"ana@example.com","document":"529.982.247-25","phone":"(11) 99988-7766"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	payload := pub.Last().Event.Payload.(events.CreateAccountPayload)
	assert.Equal(t, "52998224725", payload.Document)
	assert.Equal(t, "11999887766", payload.Phone)
}

func TestCreateAccountRejectsMalformedJSON(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", `{"user_id":`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
	assert.Empty(t, pub.Published)
}

func TestCreateAccountRejectsUnknownFields(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", `{"user_id":"x","admin":true}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
}

func TestCreateAccountRejectsOversizedBody(t *testing.T) {
	pub := &tests.FakePublisher{}
	oversized := `{"name":"` + strings.Repeat("a", 1100*1024) + `"}`

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", oversized)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "PAYLOAD_TOO_LARGE", errorCode(body))
	assert.Empty(t, pub.Published)
}

func TestCreateAccountValidatesFields(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", `{"user_id":"nope","account_type":"gold","name":"A","email":"bad","document":"123","phone":"1"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	details := errorDetails(body)
	assert.Equal(t, "uuid", details["user_id"])
	assert.Equal(t, "oneof", details["account_type"])
	assert.Equal(t, "min", details["name"])
	assert.Equal(t, "email", details["email"])
	assert.Equal(t, "cpf|cnpj", details["document"])
	assert.Equal(t, "phone_br", details["phone"])
	assert.Empty(t, pub.Published)
}

func TestCreateAccountReturnsPublisherError(t *testing.T) {
	pub := &tests.FakePublisher{Err: apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")}

	rec, body := call(accountRouter(pub), http.MethodPost, "/accounts", validCreateAccountBody(tests.UUID()))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "PUBLISH_FAILED", errorCode(body))
}

func TestUpdateAccountPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	accountID := tests.UUID()

	rec, _ := call(accountRouter(pub), http.MethodPatch, "/accounts/"+accountID, `{"name":"Ana Lima","status":"blocked"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	cmd := pub.Last()
	assert.Equal(t, events.Topics.AccountCommands, cmd.Topic)
	assert.Equal(t, accountID, cmd.Key)
	assert.Equal(t, events.EventTypes.UpdateAccount, cmd.Event.Type)

	payload := cmd.Event.Payload.(events.UpdateAccountPayload)
	assert.Equal(t, accountID, payload.AccountID)
	assert.Equal(t, "Ana Lima", *payload.Name)
	assert.Equal(t, "blocked", *payload.Status)
	assert.Nil(t, payload.Email)
	assert.Nil(t, payload.Phone)
}

func TestUpdateAccountNormalizesPhone(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, _ := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{"phone":"(11) 99988-7766"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	payload := pub.Last().Event.Payload.(events.UpdateAccountPayload)
	assert.Equal(t, "11999887766", *payload.Phone)
}

func TestUpdateAccountRejectsInvalidID(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPatch, "/accounts/not-a-uuid", `{"name":"Ana Lima"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "uuid", errorDetails(body)["id"])
	assert.Empty(t, pub.Published)
}

func TestUpdateAccountRejectsMalformedJSON(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
}

func TestUpdateAccountRejectsEmptyBody(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "EMPTY_UPDATE", errorCode(body))
	assert.Empty(t, pub.Published)
}

func TestUpdateAccountValidatesFields(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{"email":"nope","status":"deleted"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	details := errorDetails(body)
	assert.Equal(t, "email", details["email"])
	assert.Equal(t, "oneof", details["status"])
}

func TestUpdateAccountRejectsClosingThroughPatch(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{"status":"closed"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "oneof", errorDetails(body)["status"])
	assert.Empty(t, pub.Published)
}

func TestUpdateAccountReturnsPublisherError(t *testing.T) {
	pub := &tests.FakePublisher{Err: apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")}

	rec, _ := call(accountRouter(pub), http.MethodPatch, "/accounts/"+tests.UUID(), `{"name":"Ana Lima"}`)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDeleteAccountPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	accountID := tests.UUID()

	rec, body := call(accountRouter(pub), http.MethodDelete, "/accounts/"+accountID, "")

	assert.Equal(t, http.StatusAccepted, rec.Code)
	cmd := pub.Last()
	assert.Equal(t, events.Topics.AccountCommands, cmd.Topic)
	assert.Equal(t, accountID, cmd.Key)
	assert.Equal(t, events.EventTypes.DeleteAccount, cmd.Event.Type)
	assert.Equal(t, accountID, cmd.Event.Payload.(events.DeleteAccountPayload).AccountID)
	assert.Equal(t, "req-1", body["data"].(map[string]interface{})["trace_id"])
}

func TestDeleteAccountRejectsInvalidID(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(accountRouter(pub), http.MethodDelete, "/accounts/123", "")

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "uuid", errorDetails(body)["id"])
	assert.Empty(t, pub.Published)
}
