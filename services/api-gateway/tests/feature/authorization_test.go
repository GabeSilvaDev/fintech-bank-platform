package feature

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type upstreamReply struct {
	status int
	body   string
}

type fakeUpstream struct {
	mu      sync.Mutex
	replies map[string]upstreamReply
	paths   []string
}

func newFakeUpstream() *fakeUpstream {
	return &fakeUpstream{replies: make(map[string]upstreamReply)}
}

func (f *fakeUpstream) reply(path string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[path] = upstreamReply{status: status, body: body}
}

func (f *fakeUpstream) served() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func (f *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	reply, ok := f.replies[r.URL.Path]
	f.mu.Unlock()
	if !ok {
		reply = upstreamReply{status: http.StatusOK, body: `{"success":true,"data":[]}`}
	}
	writeJSON(w, reply.status, reply.body)
}

type AuthorizationTestSuite struct {
	tests.TestCase
	upstream *fakeUpstream
	server   *httptest.Server
}

func TestAuthorizationSuite(t *testing.T) {
	suite.Run(t, new(AuthorizationTestSuite))
}

func (s *AuthorizationTestSuite) SetupTest() {
	s.TestCase.SetupTest()
	s.upstream = newFakeUpstream()
	s.server = httptest.NewServer(s.upstream)
	s.useUpstream(func(*appHttp.Dependencies) {})
}

func (s *AuthorizationTestSuite) TearDownTest() {
	s.server.Close()
}

func (s *AuthorizationTestSuite) useUpstream(configure func(*appHttp.Dependencies)) {
	s.Rebuild(func(_ *config.Config, deps *appHttp.Dependencies) {
		upstream := tests.MustURL(s.server.URL)
		deps.AccountService = upstream
		deps.TransactionService = upstream
		deps.PaymentService = upstream
		deps.NotificationService = upstream
		configure(deps)
	})
}

func (s *AuthorizationTestSuite) assertForbidden(response *tests.TestResponse) {
	response.AssertForbidden().
		AssertError().
		AssertErrorCode("FORBIDDEN").
		AssertErrorMessage("access to this resource is not allowed").
		AssertHeaderExists("X-Request-ID")
}

func transactionCommand(accountID string) map[string]interface{} {
	return map[string]interface{}{
		"account_id": accountID, "type": "deposit", "amount": "10.00", "currency": "BRL", "idempotency_key": tests.RandomString(12),
	}
}

func transferCommand(accountID string) map[string]interface{} {
	return map[string]interface{}{
		"from_account_id": accountID, "to_account_id": tests.UUID(), "amount": "10.00", "currency": "BRL", "idempotency_key": tests.RandomString(12),
	}
}

func paymentCommand(accountID string) map[string]interface{} {
	return map[string]interface{}{
		"account_id": accountID, "payment_method": "pix", "amount": "10.00", "currency": "BRL",
		"recipient": "Mercado Y", "pix_key": "11999887766", "idempotency_key": tests.RandomString(12),
	}
}

func (s *AuthorizationTestSuite) TestUserScopedReadsAreLimitedToTheCaller() {
	for _, suffix := range []string{"/accounts", "/notifications"} {
		s.Get("/api/v1/users/" + s.UserID.String() + suffix).AssertOk()
		s.assertForbidden(s.Get("/api/v1/users/" + tests.UUID() + suffix))
		s.assertForbidden(s.Get("/api/v1/users/not-a-uuid" + suffix))
	}

	s.Equal([]string{"/users/" + s.UserID.String() + "/accounts", "/users/" + s.UserID.String() + "/notifications"}, s.upstream.served())
	s.Empty(s.Owners.Lookups)
}

func (s *AuthorizationTestSuite) TestAccountScopedReadsAreLimitedToTheOwner() {
	own := s.OwnedAccount()
	foreign := s.ForeignAccount()

	for _, suffix := range []string{"", "/transactions", "/payments"} {
		s.Get("/api/v1/accounts/" + own + suffix + "?limit=5").AssertOk().AssertSuccess()
		s.assertForbidden(s.Get("/api/v1/accounts/" + foreign + suffix))
	}

	s.Equal([]string{"/accounts/" + own, "/accounts/" + own + "/transactions", "/accounts/" + own + "/payments"}, s.upstream.served())
}

func (s *AuthorizationTestSuite) TestAccountScopedReadsOfAnUnknownAccountAreNotFound() {
	unknown := tests.UUID()

	for _, suffix := range []string{"", "/transactions", "/payments"} {
		s.Get("/api/v1/accounts/" + unknown + suffix).
			AssertNotFound().
			AssertErrorCode("ACCOUNT_NOT_FOUND").
			AssertErrorMessage("account not found")
	}

	s.Empty(s.upstream.served())
	s.Equal([]uuid.UUID{uuid.MustParse(unknown), uuid.MustParse(unknown), uuid.MustParse(unknown)}, s.Owners.Lookups)
}

func (s *AuthorizationTestSuite) TestPercentEncodedAccountIDsNeverReachAnotherUsersData() {
	foreign := s.ForeignAccount()
	own := s.OwnedAccount()
	encode := func(accountID string) string {
		return fmt.Sprintf("%%%x", accountID[0]) + accountID[1:]
	}

	for _, route := range []struct{ suffix, param string }{{"", "id"}, {"/transactions", "account_id"}, {"/payments", "account_id"}} {
		s.assertForbidden(s.Get("/api/v1/accounts/" + encode(foreign) + route.suffix))
		s.Get("/api/v1/accounts/"+encode(own)+route.suffix).
			AssertUnprocessableEntity().
			AssertErrorCode("VALIDATION_ERROR").
			AssertJsonPath("error.details."+route.param, "uuid")
		s.Get("/api/v1/accounts/" + encode(tests.UUID()) + route.suffix).AssertNotFound().AssertErrorCode("ACCOUNT_NOT_FOUND")
		s.Get("/api/v1/accounts/not%2Da-uuid"+route.suffix).AssertUnprocessableEntity().AssertJsonPath("error.details."+route.param, "uuid")
	}
	s.Patch("/api/v1/accounts/"+encode(foreign), map[string]string{"status": "blocked"}).AssertUnprocessableEntity()
	s.Delete("/api/v1/accounts/" + encode(foreign)).AssertUnprocessableEntity()

	s.Empty(s.upstream.served())
	s.Empty(s.Publisher.Published)
}

func (s *AuthorizationTestSuite) TestAccountScopedReadsRejectUnparseableIDsAtTheGateway() {
	for _, route := range []struct{ suffix, param string }{{"", "id"}, {"/transactions", "account_id"}, {"/payments", "account_id"}} {
		s.Get("/api/v1/accounts/not-a-uuid"+route.suffix).
			AssertUnprocessableEntity().
			AssertErrorCode("VALIDATION_ERROR").
			AssertErrorMessage("request validation failed").
			AssertJsonPath("error.details."+route.param, "uuid")
	}

	s.Empty(s.upstream.served())
	s.Empty(s.Owners.Lookups)
}

func (s *AuthorizationTestSuite) TestAccountCommandsAreLimitedToTheOwner() {
	own := s.OwnedAccount()
	foreign := s.ForeignAccount()
	unknown := tests.UUID()

	s.Patch("/api/v1/accounts/"+own, map[string]string{"status": "blocked"}).AssertAccepted()
	s.Delete("/api/v1/accounts/" + own).AssertAccepted()

	s.assertForbidden(s.Patch("/api/v1/accounts/"+foreign, map[string]string{"status": "blocked"}))
	s.assertForbidden(s.Delete("/api/v1/accounts/" + foreign))

	s.Patch("/api/v1/accounts/"+unknown, map[string]string{"status": "blocked"}).
		AssertNotFound().
		AssertErrorCode("ACCOUNT_NOT_FOUND").
		AssertErrorMessage("account not found")
	s.Delete("/api/v1/accounts/" + unknown).AssertNotFound().AssertErrorCode("ACCOUNT_NOT_FOUND")

	s.Patch("/api/v1/accounts/not-a-uuid", map[string]string{"status": "blocked"}).AssertUnprocessableEntity().AssertJsonPath("error.details.id", "uuid")
	s.Delete("/api/v1/accounts/not-a-uuid").AssertUnprocessableEntity().AssertJsonPath("error.details.id", "uuid")

	s.Len(s.Publisher.Published, 2)
	s.Equal(own, s.Publisher.Published[0].Key)
	s.Equal(own, s.Publisher.Published[1].Key)
}

func (s *AuthorizationTestSuite) TestInvalidAccountCommandsAreRejectedBeforeTheOwnerLookup() {
	s.Patch("/api/v1/accounts/"+s.ForeignAccount(), map[string]string{}).AssertUnprocessableEntity().AssertErrorCode("EMPTY_UPDATE")
	s.Post("/api/v1/transactions", map[string]interface{}{"account_id": s.ForeignAccount()}).AssertUnprocessableEntity()

	s.Empty(s.Owners.Lookups)
}

func (s *AuthorizationTestSuite) TestMoneyCommandsNeedAnAccountOfTheCaller() {
	commands := []struct {
		path string
		body func(string) map[string]interface{}
	}{
		{"/api/v1/transactions", transactionCommand},
		{"/api/v1/transfers", transferCommand},
		{"/api/v1/payments", paymentCommand},
	}

	for _, command := range commands {
		own := s.OwnedAccount()
		s.Post(command.path, command.body(own)).AssertAccepted()
		s.Equal(own, s.Publisher.Last().Key, command.path)

		s.assertForbidden(s.Post(command.path, command.body(s.ForeignAccount())))
		s.Post(command.path, command.body(tests.UUID())).
			AssertNotFound().
			AssertErrorCode("ACCOUNT_NOT_FOUND")
	}

	s.Len(s.Publisher.Published, 3)
}

func (s *AuthorizationTestSuite) TestTransfersMayGoToAnotherUsersAccount() {
	from := s.OwnedAccount()
	to := s.ForeignAccount()

	s.Post("/api/v1/transfers", map[string]interface{}{
		"from_account_id": from, "to_account_id": to, "amount": "10.00", "currency": "BRL", "idempotency_key": "tr-foreign",
	}).AssertAccepted()

	payload := s.Publisher.Last().Event.Payload.(events.ProcessTransferPayload)
	s.Equal(to, payload.ToAccountID)
	s.Equal([]uuid.UUID{uuid.MustParse(from)}, s.Owners.Lookups)
}

func (s *AuthorizationTestSuite) TestAccountCreationIsForTheCaller() {
	account := map[string]interface{}{"account_type": "checking", "name": "Ana Souza", "email": "ana@example.com", "document": "52998224725"}

	s.Post("/api/v1/accounts", account).AssertAccepted()
	s.Equal(s.UserID.String(), s.Publisher.Last().Event.Payload.(events.CreateAccountPayload).UserID)

	account["user_id"] = s.UserID.String()
	s.Post("/api/v1/accounts", account).AssertAccepted()
	s.Equal(s.UserID.String(), s.Publisher.Last().Key)

	account["user_id"] = tests.UUID()
	s.assertForbidden(s.Post("/api/v1/accounts", account))

	account["user_id"] = "not-a-uuid"
	s.Post("/api/v1/accounts", account).AssertUnprocessableEntity().AssertJsonPath("error.details.user_id", "uuid")

	s.Len(s.Publisher.Published, 2)
}

func (s *AuthorizationTestSuite) TestTransactionReadsNeedTheAccountOrTheCounterparty() {
	own := s.OwnedAccount()
	foreign := s.ForeignAccount()
	another := s.ForeignAccount()
	ownBody := `{"success":true,"data":{"transaction_id":"t1","account_id":"` + own + `","type":"deposit"}}`
	incomingBody := `{"success":true,"data":{"transaction_id":"t2","account_id":"` + foreign + `","counterparty_id":"` + own + `","type":"transfer_out"}}`
	s.upstream.reply("/transactions/t1", http.StatusOK, ownBody)
	s.upstream.reply("/transactions/t2", http.StatusOK, incomingBody)
	s.upstream.reply("/transactions/t3", http.StatusOK, `{"success":true,"data":{"account_id":"`+foreign+`","counterparty_id":"`+another+`"}}`)
	s.upstream.reply("/transactions/t4", http.StatusOK, `{"success":true,"data":{"account_id":"`+tests.UUID()+`"}}`)
	s.upstream.reply("/transactions/t5", http.StatusNotFound, `{"success":false,"error":{"code":"TRANSACTION_NOT_FOUND","message":"transaction not found"}}`)

	s.Equal(ownBody, s.Get("/api/v1/transactions/t1").AssertOk().Body())
	s.Equal(incomingBody, s.Get("/api/v1/transactions/t2").AssertOk().Body())
	s.assertForbidden(s.Get("/api/v1/transactions/t3"))
	s.assertForbidden(s.Get("/api/v1/transactions/t4"))
	s.Get("/api/v1/transactions/t5").AssertNotFound().AssertErrorCode("TRANSACTION_NOT_FOUND")
}

func (s *AuthorizationTestSuite) TestTransferReadsHideSenderOnlyFieldsFromTheCounterparty() {
	own := s.OwnedAccount()
	foreign := s.ForeignAccount()
	sent := `{"success":true,"data":{"transaction_id":"t1","type":"transfer","account_id":"` + own + `","counterparty_id":"` + foreign + `","description":"rent","idempotency_key":"k-1"}}`
	received := `{"success":true,"data":{"transaction_id":"t2","type":"transfer","account_id":"` + foreign + `","counterparty_id":"` + own + `","description":"rent","idempotency_key":"k-2"}}`
	s.upstream.reply("/transactions/t1", http.StatusOK, sent)
	s.upstream.reply("/transactions/t2", http.StatusOK, received)

	s.Equal(sent, s.Get("/api/v1/transactions/t1").AssertOk().Body())
	s.Get("/api/v1/transactions/t2").
		AssertOk().
		AssertJsonPath("data.transaction_id", "t2").
		AssertJsonPath("data.account_id", foreign).
		AssertJsonPath("data.counterparty_id", own).
		AssertJsonMissing("data.description").
		AssertJsonMissing("data.idempotency_key")
}

func (s *AuthorizationTestSuite) TestPaymentReadsNeedTheAccount() {
	own := s.OwnedAccount()
	foreign := s.ForeignAccount()
	ownBody := `{"success":true,"data":{"payment_id":"p1","account_id":"` + own + `"}}`
	s.upstream.reply("/payments/p1", http.StatusOK, ownBody)
	s.upstream.reply("/payments/p2", http.StatusOK, `{"success":true,"data":{"payment_id":"p2","account_id":"`+foreign+`","counterparty_id":"`+own+`"}}`)
	s.upstream.reply("/payments/p3", http.StatusNotFound, `{"success":false,"error":{"code":"PAYMENT_NOT_FOUND","message":"payment not found"}}`)

	s.Equal(ownBody, s.Get("/api/v1/payments/p1").AssertOk().Body())
	s.assertForbidden(s.Get("/api/v1/payments/p2"))
	s.Get("/api/v1/payments/p3").AssertNotFound().AssertErrorCode("PAYMENT_NOT_FOUND")
}

func (s *AuthorizationTestSuite) TestOwnerLookupFailuresAreBadGateway() {
	s.Owners.Err = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)
	s.upstream.reply("/transactions/t1", http.StatusOK, `{"success":true,"data":{"account_id":"`+tests.UUID()+`"}}`)
	id := tests.UUID()

	s.Get("/api/v1/accounts/" + id).AssertStatus(http.StatusBadGateway).AssertErrorMessage("account service is unavailable")
	s.Delete("/api/v1/accounts/" + id).AssertStatus(http.StatusBadGateway).AssertErrorCode("UPSTREAM_UNAVAILABLE")
	s.Post("/api/v1/transactions", transactionCommand(id)).AssertStatus(http.StatusBadGateway)
	s.Get("/api/v1/transactions/t1").AssertStatus(http.StatusBadGateway).AssertErrorMessage("account service is unavailable")

	s.Empty(s.Publisher.Published)
	s.Equal([]string{"/transactions/t1"}, s.upstream.served())
}

func (s *AuthorizationTestSuite) TestTheDefaultOwnerClientAsksTheAccountServiceOnce() {
	accountID := tests.UUID()
	s.upstream.reply("/accounts/"+accountID+"/owner", http.StatusOK, `{"success":true,"data":{"account_id":"`+accountID+`","user_id":"`+s.UserID.String()+`","name":"Ana","email":"ana@example.com"}}`)
	s.useUpstream(func(deps *appHttp.Dependencies) { deps.Owners = nil })

	s.Get("/api/v1/accounts/" + accountID).AssertOk()
	s.Post("/api/v1/transactions", transactionCommand(accountID)).AssertAccepted()
	s.ActingAs(uuid.New())
	s.assertForbidden(s.Get("/api/v1/accounts/" + accountID + "/transactions"))

	s.Equal([]string{"/accounts/" + accountID + "/owner", "/accounts/" + accountID}, s.upstream.served())
}
