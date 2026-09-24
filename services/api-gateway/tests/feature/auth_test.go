package feature

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	mu           sync.Mutex
	identities   map[string]identityRecord
	sessions     map[string]*sessionRecord
	issued       int
	calls        int
	sessionCalls int
	status       int
	retryAfter   string
	lockedFor    string
	reads        []string
}

type identityRecord struct {
	userID   uuid.UUID
	password string
}

type sessionRecord struct {
	userID uuid.UUID
	family string
	active bool
}

func newFakeAccountService() *fakeAccountService {
	return &fakeAccountService{identities: make(map[string]identityRecord), sessions: make(map[string]*sessionRecord)}
}

func (f *fakeAccountService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method == http.MethodGet {
		f.reads = append(f.reads, r.URL.Path)
		writeJSON(w, http.StatusOK, `{"success":true,"data":[]}`)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/sessions") {
		f.sessionCalls++
	} else {
		f.calls++
	}
	if f.status != 0 {
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}
		writeJSON(w, f.status, `{"success":false,"error":{"code":"UPSTREAM","message":"upstream"}}`)
		return
	}

	var body struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		UserID       string `json:"user_id"`
		RefreshToken string `json:"refresh_token"`
	}
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
		if f.lockedFor != "" {
			w.Header().Set("Retry-After", f.lockedFor)
			writeJSON(w, http.StatusTooManyRequests, `{"success":false,"error":{"code":"TOO_MANY_ATTEMPTS","message":"too many failed attempts; try again later"}}`)
			return
		}
		record, ok := f.identities[body.Email]
		if !ok || record.password != body.Password {
			writeJSON(w, http.StatusUnauthorized, `{"success":false,"error":{"code":"INVALID_CREDENTIALS","message":"invalid email or password"}}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"success":true,"data":{"user_id":"`+record.userID.String()+`"}}`)
	case "/sessions":
		token := f.issue(uuid.MustParse(body.UserID), uuid.NewString())
		writeJSON(w, http.StatusCreated, `{"success":true,"data":{"refresh_token":"`+token+`","expires_at":"`+sessionExpiry()+`"}}`)
	case "/sessions/rotate":
		session, ok := f.sessions[body.RefreshToken]
		if !ok || !session.active {
			if ok {
				f.revokeFamily(session.family)
			}
			writeJSON(w, http.StatusUnauthorized, `{"success":false,"error":{"code":"INVALID_SESSION","message":"refresh token is invalid or expired"}}`)
			return
		}
		session.active = false
		token := f.issue(session.userID, session.family)
		writeJSON(w, http.StatusOK, `{"success":true,"data":{"user_id":"`+session.userID.String()+`","refresh_token":"`+token+`","expires_at":"`+sessionExpiry()+`"}}`)
	case "/sessions/revoke":
		if session, ok := f.sessions[body.RefreshToken]; ok {
			f.revokeFamily(session.family)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeAccountService) issue(userID uuid.UUID, family string) string {
	f.issued++
	token := "refresh-" + strconv.Itoa(f.issued)
	f.sessions[token] = &sessionRecord{userID: userID, family: family, active: true}
	return token
}

func (f *fakeAccountService) revokeFamily(family string) {
	for _, session := range f.sessions {
		if session.family == family {
			session.active = false
		}
	}
}

func sessionExpiry() string {
	return time.Now().Add(720*time.Hour + 30*time.Second).UTC().Format(time.RFC3339Nano)
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

func refreshToken(token string) map[string]interface{} {
	return map[string]interface{}{"refresh_token": token}
}

func dataString(response *tests.TestResponse, key string) string {
	return response.Json()["data"].(map[string]interface{})[key].(string)
}

func (s *AuthTestSuite) assertRefreshExpiresInAboutThirtyDays(response *tests.TestResponse) {
	seconds := response.Json()["data"].(map[string]interface{})["refresh_expires_in"].(float64)
	s.InDelta(float64(720*3600), seconds, 60)
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

	s.Post("/api/v1/auth/refresh", refreshToken("refresh-1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE").
		AssertHeaderMissing("WWW-Authenticate")
	s.Post("/api/v1/auth/logout", refreshToken("refresh-1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")

	s.server.Close()
	s.Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")
	s.Post("/api/v1/auth/logout", refreshToken("refresh-1")).
		AssertStatus(http.StatusBadGateway).
		AssertErrorCode("UPSTREAM_UNAVAILABLE")
}

func (s *AuthTestSuite) TestRegisterAndLoginIssueRefreshTokens() {
	email := tests.RandomEmail()

	registered := s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).
		AssertCreated().
		AssertJsonPath("data.refresh_token", "refresh-1")
	s.assertRefreshExpiresInAboutThirtyDays(registered)

	loggedIn := s.Post("/api/v1/auth/login", credentials(email, "password1")).
		AssertOk().
		AssertJsonPath("data.refresh_token", "refresh-2")
	s.assertRefreshExpiresInAboutThirtyDays(loggedIn)

	s.Equal(2, s.accounts.sessionCalls)
	s.Equal(s.accounts.identities[email].userID, s.accounts.sessions["refresh-1"].userID)
	s.NotEqual(s.accounts.sessions["refresh-1"].family, s.accounts.sessions["refresh-2"].family)
}

func (s *AuthTestSuite) TestRefreshRotatesTheTokenPair() {
	email := tests.RandomEmail()
	first := dataString(s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated(), "refresh_token")

	response := s.Post("/api/v1/auth/refresh", refreshToken(first)).
		AssertOk().
		AssertJsonPath("data.token_type", "Bearer").
		AssertJsonPath("data.expires_in", float64(3600)).
		AssertJsonPath("data.refresh_token", "refresh-2").
		AssertJsonHas("data.access_token").
		AssertJsonMissing("data.user_id")
	s.assertRefreshExpiresInAboutThirtyDays(response)

	userID := s.accounts.identities[email].userID.String()
	s.WithToken(dataString(response, "access_token")).Get("/api/v1/users/" + userID + "/accounts").AssertOk()
}

func (s *AuthTestSuite) TestReusingARotatedRefreshTokenRevokesTheSession() {
	first := dataString(s.WithoutToken().Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).AssertCreated(), "refresh_token")
	second := dataString(s.Post("/api/v1/auth/refresh", refreshToken(first)).AssertOk(), "refresh_token")

	s.Post("/api/v1/auth/refresh", refreshToken(first)).
		AssertUnauthorized().
		AssertErrorCode("INVALID_SESSION").
		AssertErrorMessage("refresh token is invalid or expired").
		AssertHeader("WWW-Authenticate", "Bearer").
		AssertJsonMissing("data")
	s.Post("/api/v1/auth/refresh", refreshToken(second)).
		AssertUnauthorized().
		AssertErrorCode("INVALID_SESSION")
}

func (s *AuthTestSuite) TestLogoutRevokesTheSession() {
	email := tests.RandomEmail()
	s.WithoutToken().Post("/api/v1/auth/register", credentials(email, "password1")).AssertCreated()
	token := dataString(s.Post("/api/v1/auth/login", credentials(email, "password1")).AssertOk(), "refresh_token")

	s.Post("/api/v1/auth/logout", refreshToken(token)).AssertNoContent()
	s.Post("/api/v1/auth/refresh", refreshToken(token)).
		AssertUnauthorized().
		AssertErrorCode("INVALID_SESSION")
	s.Post("/api/v1/auth/refresh", refreshToken("refresh-1")).AssertOk()
}

func (s *AuthTestSuite) TestLogoutIsIdempotent() {
	token := dataString(s.WithoutToken().Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).AssertCreated(), "refresh_token")

	s.Post("/api/v1/auth/logout", refreshToken(token)).AssertNoContent()
	s.Post("/api/v1/auth/logout", refreshToken(token)).AssertNoContent()
	s.Post("/api/v1/auth/logout", refreshToken("never-issued")).AssertNoContent()
}

func (s *AuthTestSuite) TestRefreshAndLogoutValidateWithoutCallingTheAccountService() {
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout"} {
		s.WithoutToken().Post(path, map[string]interface{}{}).
			AssertUnprocessableEntity().
			AssertErrorCode("VALIDATION_ERROR").
			AssertJsonPath("error.details.refresh_token", "required")
		s.Post(path, refreshToken("")).AssertUnprocessableEntity()
		s.Post(path, map[string]interface{}{"refresh_token": "refresh-1", "user_id": "x"}).AssertBadRequest()
	}

	s.Equal(0, s.accounts.sessionCalls)
}

func (s *AuthTestSuite) TestLoginForwardsALockout() {
	s.accounts.lockedFor = "840"

	s.WithoutToken().Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertTooManyRequests().
		AssertErrorCode("TOO_MANY_ATTEMPTS").
		AssertHeader("Retry-After", "840").
		AssertHeaderMissing("WWW-Authenticate")
	s.Equal(0, s.accounts.sessionCalls)
}

func (s *AuthTestSuite) TestABusyAccountServiceAsksClientsToRetry() {
	s.accounts.status = http.StatusServiceUnavailable
	s.accounts.retryAfter = "1"

	s.WithoutToken().Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusServiceUnavailable).
		AssertErrorCode("SERVICE_BUSY").
		AssertHeader("Retry-After", "1")
	s.Post("/api/v1/auth/login", credentials(tests.RandomEmail(), "password1")).
		AssertStatus(http.StatusServiceUnavailable).
		AssertHeader("Retry-After", "1")
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

	s.Post("/api/v1/auth/refresh", refreshToken("refresh-1")).AssertTooManyRequests()
	s.Post("/api/v1/auth/logout", refreshToken("refresh-1")).AssertTooManyRequests()

	s.Equal(3, s.accounts.calls)
	s.Equal(2, s.accounts.sessionCalls)
	s.Get("/health").AssertOk()
	s.ActingAs(uuid.New()).Get("/api/v1/accounts/" + s.OwnedAccount()).AssertOk()
}

func (s *AuthTestSuite) TestAuthRoutesDoNotRequireAToken() {
	s.WithHeader("Authorization", "Bearer garbage").
		Post("/api/v1/auth/register", credentials(tests.RandomEmail(), "password1")).
		AssertCreated()
	s.Post("/api/v1/auth/refresh", refreshToken("refresh-1")).AssertOk()
	s.Post("/api/v1/auth/logout", refreshToken("refresh-2")).AssertNoContent()
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
