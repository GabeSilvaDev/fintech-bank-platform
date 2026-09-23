package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func historyRouter(history *tests.FakeHistory) http.Handler {
	h := handlers.NewHistoryHandler(history)
	r := chi.NewRouter()
	r.Get("/users/{user_id}/notifications", h.ListUserNotifications)
	return r
}

func getHistory(handler http.Handler, path string) (*httptest.ResponseRecorder, map[string]interface{}) {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec, tests.FromJson(rec.Body.String())
}

var historySentAt = time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)

func TestListUserNotificationsShape(t *testing.T) {
	userID := uuid.New()
	full := models.Record{ID: "n1", UserID: userID, Channel: models.ChannelEmail, Recipient: "ana@example.com", Subject: "Bem-vindo(a)", Body: "Ola Ana", SourceEventID: "evt-1", SentAt: historySentAt}
	bare := models.Record{ID: "n2", UserID: userID, Channel: models.ChannelSMS, Recipient: "+5511999887766", Body: "Codigo 1234", SentAt: historySentAt}
	history := &tests.FakeHistory{Records: []models.Record{full, bare}}

	rec, body := getHistory(historyRouter(history), "/users/"+userID.String()+"/notifications")
	assert.Equal(t, http.StatusOK, rec.Code)

	data := body["data"].([]interface{})
	assert.Len(t, data, 2)

	item := data[0].(map[string]interface{})
	assert.Equal(t, "n1", item["id"])
	assert.Equal(t, "email", item["channel"])
	assert.Equal(t, "ana@example.com", item["recipient"])
	assert.Equal(t, "Bem-vindo(a)", item["subject"])
	assert.Equal(t, "Ola Ana", item["body"])
	assert.Equal(t, "evt-1", item["source_event_id"])
	assert.Equal(t, "2026-09-20T08:30:00Z", item["sent_at"])

	bareItem := data[1].(map[string]interface{})
	assert.Equal(t, "n2", bareItem["id"])
	assert.NotContains(t, bareItem, "subject")
	assert.NotContains(t, bareItem, "source_event_id")
}

func TestListUserNotificationsLimitDefaultAndBoundaries(t *testing.T) {
	userID := uuid.New()
	records := make([]models.Record, 0, 25)
	for i := 0; i < 25; i++ {
		records = append(records, models.Record{ID: uuid.NewString(), UserID: userID, Channel: models.ChannelPush, Recipient: "device", Body: "hi", SentAt: historySentAt})
	}
	history := &tests.FakeHistory{Records: records}
	router := historyRouter(history)

	rec, body := getHistory(router, "/users/"+userID.String()+"/notifications")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 20)

	rec, body = getHistory(router, "/users/"+userID.String()+"/notifications?limit=1")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 1)

	rec, body = getHistory(router, "/users/"+userID.String()+"/notifications?limit=100")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 25)
}

func TestListUserNotificationsInvalidLimit(t *testing.T) {
	userID := uuid.New()
	router := historyRouter(&tests.FakeHistory{})

	for _, bad := range []string{"0", "101", "x"} {
		rec, body := getHistory(router, "/users/"+userID.String()+"/notifications?limit="+bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, bad)
		errBody := body["error"].(map[string]interface{})
		assert.Equal(t, "VALIDATION_ERROR", errBody["code"], bad)
		assert.Equal(t, "range", errBody["details"].(map[string]interface{})["limit"], bad)
	}
}

func TestListUserNotificationsInvalidUserID(t *testing.T) {
	router := historyRouter(&tests.FakeHistory{})

	rec, body := getHistory(router, "/users/not-a-uuid/notifications")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	errBody := body["error"].(map[string]interface{})
	assert.Equal(t, "VALIDATION_ERROR", errBody["code"])
	assert.Equal(t, "uuid", errBody["details"].(map[string]interface{})["user_id"])
}

func TestListUserNotificationsHistoryError(t *testing.T) {
	userID := uuid.New()
	history := &tests.FakeHistory{Err: errors.New("cassandra down")}

	rec, _ := getHistory(historyRouter(history), "/users/"+userID.String()+"/notifications")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
