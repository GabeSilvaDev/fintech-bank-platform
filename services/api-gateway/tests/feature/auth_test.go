package feature

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type fakeAccountService struct {
	mu         sync.Mutex
	identities map[string]identityRecord
	calls      int
	status     int
	reads      []string
}

type identityRecord struct {
	userID   uuid.UUID
	password string
}

func newFakeAccountService() *fakeAccountService {
	return &fakeAccountService{identities: make(map[string]identityRecord)}
}

func (f *fakeAccountService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method == http.MethodGet {
		f.reads = append(f.reads, r.URL.Path)
		writeJSON(w, http.StatusOK, `{"success":true,"data":[]}`)
		return
	}

	f.calls++
	if f.status != 0 {
		writeJSON(w, f.status, `{"success":false,"error":{"code":"UPSTREAM","message":"upstream"}}`)
		return
	}

	var body struct{ Email, Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch r.URL.Path {
	case "/identities":
		if len(body.Password) < 8 {
			writeJSON(w, http.StatusUnprocessableEntity, `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"request validation failed","details":{"password":"length"}}}`)
			return
		}
		if _, taken := f.identities[body.Email]; taken {
			writeJSON(w, http.StatusConflict, `{"success":false,"error":{"code":"EMAIL_TAKEN","message":"email already registered"}}`)
			return
		}
		record := identityRecord{userID: uuid.New(), password: body.Password}
		f.identities[body.Email] = record
		writeJSON(w, http.StatusCreated, `{"success":true,"data":{"user_id":"`+record.userID.String()+`"}}`)
	case "/identities/verify":
		record, ok := f.identities[body.Email]
		if !ok || record.password != body.Password {
			writeJSON(w, http.StatusUnauthorized, `{"success":false,"error":{"code":"INVALID_CREDENTIALS","message":"invalid email or password"}}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"success":true,"data":{"user_id":"`+record.userID.String()+`"}}`)
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

type AuthTestSuite struct {
	tests.TestCase
	accounts *fakeAccountService
	server   *httptest.Server
}

func TestAuthSuite(t *testing.T) {
	suite.Run(t, new(AuthTestSuite))
}

func (s *AuthTestSuite) SetupTest() {
	s.TestCase.SetupTest()
	s.accounts = newFakeAccountService()
	s.server = httptest.NewServer(s.accounts)
	s.useAuthLimit(1000)
}

func (s *AuthTestSuite) TearDownTest() {
	s.server.Close()
}

func (s *AuthTestSuite) useAuthLimit(requests int) {
	s.Rebuild(func(cfg *config.Config, deps *appHttp.Dependencies) {
		cfg.AuthRateLimit = contracts.RateLimitConfig{Requests: requests, Window: time.Minute}
		deps.AccountService = tests.MustURL(s.server.URL)
	})
}

func credentials(email, password string) map[string]interface{} {
	return map[string]interface{}{"email": email, "password": password}
}

func (s *AuthTestSuite) TestRegisterIssuesAWorkingToken() {
	email := tests.RandomEmail()

	response := s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).
		AssertCreated().
		AssertSuccess().
		AssertJsonPath("data.token_type", "Bearer").
		AssertJsonPath("data.expires_in", float64(3600)).
		AssertJsonHas("data.access_token").
		AssertJsonHas("data.user_id")

	userID := response.Json()["data"].(map[string]interface{})["user_id"].(string)
	token := response.Json()["data"].(map[string]interface{})["access_token"].(string)
	s.Equal(s.accounts.identities[email].userID.String(), userID)

	s.WithToken(token).Get("/api/v1/users/" + userID + "/accounts").AssertOk()
	s.Equal([]string{"/users/" + userID + "/accounts"}, s.accounts.reads)
}

func (s *AuthTestSuite) TestLoginIssuesAWorkingToken() {
	email := tests.RandomEmail()
	s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated()

	response := s.Post("/api/v1/auth/login", credentials(email, "password1")).
		AssertOk().
		AssertJsonPath("data.token_type", "Bearer").
		AssertJsonPath("data.expires_in", float64(3600)).
		AssertJsonMissing("data.user_id")

	token := response.Json()["data"].(map[string]interface{})["access_token"].(string)
	accountID := s.Owners.Own(tests.UUID(), s.accounts.identities[email].userID)
	s.WithToken(token).Get("/api/v1/accounts/" + accountID).AssertOk()
}

func (s *AuthTestSuite) TestRegisterTwiceIsAConflict() {
	email := tests.RandomEmail()
	s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated()

	s.Post("/api/v1/auth/register", credentials(email, "password2")).
		AssertStatus(http.StatusConflict).
		AssertErrorCode("EMAIL_TAKEN").
		AssertErrorMessage("e-mail already registered").
		AssertHeaderMissing("WWW-Authenticate")
}

func (s *AuthTestSuite) TestLoginWithAWrongPasswordIsUnauthorized() {
	email := tests.RandomEmail()
	s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated()

	s.Post("/api/v1/auth/login", credentials(email, "password2")).
		AssertUnauthorized().
		AssertErrorCode("INVALID_CREDENTIALS").
		AssertHeader("WWW-Authenticate", "Bearer").
		AssertJsonMissing("data")
	s.Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertUnauthorized().
		AssertErrorCode("INVALID_CREDENTIALS").
		AssertHeader("WWW-Authenticate", "Bearer")
}

func (s *AuthTestSuite) TestRegisterValidatesWithoutCallingTheAccountService() {
	s.WithoutToken().Post("/api/v1/auth/register", credentials("not-an-email", "short")).
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.email", "email").
		AssertJsonPath("error.details.password", "length")

	s.Equal(0, s.accounts.calls)
}

func (s *AuthTestSuite) TestUpstreamValidationDetailsPassThrough() {
	s.accounts.status = http.StatusUnprocessableEntity

	s.WithoutToken().Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR")
}

func (s *AuthTestSuite) TestAccountServiceFailuresAreBadGateway() {
	s.accounts.status = http.StatusInternalServerError

	s.WithoutToken().Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")
	s.Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")

	s.server.Close()
	s.Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")
}

func (s *AuthTestSuite) TestAuthRoutesShareAStricterRateLimit() {
	s.useAuthLimit(3)
	email := tests.RandomEmail()

	s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated()
	s.Post("/api/v1/auth/login", credentials(email, "password1")).AssertOk()
	s.Post("/api/v1/auth/login", credentials(email, "wrong-password")).AssertUnauthorized()
	s.Post("/api/v1/auth/login", credentials(email, "password1")).
		AssertTooManyRequests().
		AssertErrorCode("RATE_LIMIT_EXCEEDED")
	s.Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).AssertTooManyRequests()

	s.Equal(3, s.accounts.calls)
	s.Get("/health").AssertOk()
	s.ActingAs(uuid.New()).Get("/api/v1/accounts/" + s.OwnedAccount()).AssertOk()
}

func (s *AuthTestSuite) TestAuthRoutesDoNotRequireAToken() {
	s.WithHeader("Authorization", "Bearer garbage").
		Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).
		AssertCreated()
}

func (s *AuthTestSuite) TestProtectedRoutesRequireAValidToken() {
	protected := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/accounts"},
		{http.MethodPatch, "/api/v1/accounts/" + tests.UUID()},
		{http.MethodDelete, "/api/v1/accounts/" + tests.UUID()},
		{http.MethodGet, "/api/v1/accounts/" + tests.UUID()},
		{http.MethodGet, "/api/v1/users/" + tests.UUID() + "/accounts"},
		{http.MethodGet, "/api/v1/accounts/" + tests.UUID() + "/transactions"},
		{http.MethodPost, "/api/v1/transactions"},
		{http.MethodPost, "/api/v1/transfers"},
		{http.MethodGet, "/api/v1/transactions/" + tests.UUID()},
		{http.MethodGet, "/api/v1/accounts/" + tests.UUID() + "/payments"},
		{http.MethodPost, "/api/v1/payments"},
		{http.MethodGet, "/api/v1/payments/" + tests.UUID()},
		{http.MethodGet, "/api/v1/users/" + tests.UUID() + "/notifications"},
	}

	for _, route := range protected {
		for _, header := range []string{"", "Bearer", "Bearer not-a-token", "Basic " + tests.AccessToken(uuid.New())} {
			s.WithoutToken()
			if header != "" {
				s.WithHeader("Authorization", header)
			}

			var response *tests.TestResponse
			if route.method == http.MethodGet {
				response = s.Get(route.path)
			} else if route.method == http.MethodDelete {
				response = s.Delete(route.path)
			} else if route.method == http.MethodPatch {
				response = s.Patch(route.path, map[string]string{"status": "blocked"})
			} else {
				response = s.Post(route.path, map[string]string{})
			}

			response.AssertUnauthorized().
				AssertHeader("WWW-Authenticate", "Bearer").
				AssertErrorCode("UNAUTHORIZED").
				AssertErrorMessage("authentication required")
		}
	}

	s.Empty(s.Publisher.Published)
	s.Empty(s.accounts.reads)
}

func (s *AuthTestSuite) TestProtectedCommandsAcceptAValidToken() {
	userID := uuid.New()
	accountID := s.Owners.Own(tests.UUID(), userID)

	s.WithoutToken().WithHeader("Authorization", "bearer "+tests.AccessToken(userID)).
		Delete("/api/v1/accounts/" + accountID).
		AssertAccepted()

	s.Len(s.Publisher.Published, 1)
}

func (s *AuthTestSuite) TestPublicRoutesStayPublic() {
	s.WithoutToken().Get("/health").AssertOk()
	s.Get("/api/v1/openapi.yaml").AssertOk().AssertContentType("application/yaml")
	s.Get("/api/v1/unknown").AssertNotFound()
}
