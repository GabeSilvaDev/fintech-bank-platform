package unit

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/api"
	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	OpenAPI string                          `yaml:"openapi"`
	Servers []struct{ URL string }          `yaml:"servers"`
	Paths   map[string]map[string]yaml.Node `yaml:"paths"`
}

var (
	openAPIMethods   = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true}
	routeParamRegexp = regexp.MustCompile(`\{([^}:]+)(:[^}]*)?\}`)
)

func parseOpenAPIDocument(t *testing.T) openAPIDocument {
	t.Helper()
	var doc openAPIDocument
	require.NoError(t, yaml.Unmarshal(api.Spec, &doc))
	return doc
}

func documentedOperations(doc openAPIDocument) []string {
	var operations []string
	for path, item := range doc.Paths {
		for method := range item {
			if openAPIMethods[method] {
				operations = append(operations, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(operations)
	return operations
}

func normaliseRoute(route string) string {
	route = strings.ReplaceAll(route, "/*/", "/")
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	return routeParamRegexp.ReplaceAllString(route, "{$1}")
}

func routedOperations(t *testing.T) []string {
	t.Helper()
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}
	deps := testDependencies()
	deps.Metrics = metrics.New("test-openapi-routes")
	appHttp.SetupRouter(router, cfg, deps)

	var operations []string
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = normaliseRoute(route)
		if route == "/health" || strings.HasPrefix(route, "/api/v1/") {
			operations = append(operations, method+" "+route)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(operations)
	return operations
}

func missingFrom(expected, actual []string) []string {
	present := make(map[string]bool, len(actual))
	for _, operation := range actual {
		present[operation] = true
	}
	var missing []string
	for _, operation := range expected {
		if !present[operation] {
			missing = append(missing, operation)
		}
	}
	return missing
}

func TestOpenAPIHandlerServesTheEmbeddedDocument(t *testing.T) {
	rec := httptest.NewRecorder()

	handlers.OpenAPI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.NotEmpty(t, api.Spec)
	assert.Equal(t, api.Spec, rec.Body.Bytes())
}

func TestRouterServesTheOpenAPIDocument(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}
	appHttp.SetupRouter(router, cfg, testDependencies())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, api.Spec, rec.Body.Bytes())
}

func TestOpenAPIDocumentIsOpenAPI31ForTheLocalGateway(t *testing.T) {
	doc := parseOpenAPIDocument(t)

	assert.Equal(t, "3.1.0", doc.OpenAPI)
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "http://localhost:8081", doc.Servers[0].URL)
}

func TestOpenAPIDocumentsEveryRoutedOperation(t *testing.T) {
	documented := documentedOperations(parseOpenAPIDocument(t))
	routed := routedOperations(t)

	require.NotEmpty(t, routed)
	assert.Contains(t, routed, "GET /api/v1/openapi.yaml")
	assert.Empty(t, missingFrom(routed, documented), "routed operations missing from the OpenAPI document")
	assert.Empty(t, missingFrom(documented, routed), "documented operations that are not routed")
}

func TestOpenAPIRouteNormalisation(t *testing.T) {
	assert.Equal(t, "/api/v1/accounts/{id}", normaliseRoute("/api/v1/*/accounts/{id:[0-9a-f-]+}/"))
	assert.Equal(t, "/", normaliseRoute("/"))
	assert.Equal(t, []string{"GET /b"}, missingFrom([]string{"GET /a", "GET /b"}, []string{"GET /a"}))
}

type openAPISecurityDocument struct {
	Security   []map[string][]string `yaml:"security"`
	Paths      map[string]map[string]yaml.Node
	Components struct {
		SecuritySchemes map[string]struct {
			Type         string `yaml:"type"`
			Scheme       string `yaml:"scheme"`
			BearerFormat string `yaml:"bearerFormat"`
		} `yaml:"securitySchemes"`
		Schemas map[string]struct {
			Required []string `yaml:"required"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPIOperation struct {
	Security  *[]map[string][]string `yaml:"security"`
	Responses map[string]yaml.Node   `yaml:"responses"`
}

func TestOpenAPIDocumentProtectsEveryOperationButThePublicOnes(t *testing.T) {
	var doc openAPISecurityDocument
	require.NoError(t, yaml.Unmarshal(api.Spec, &doc))

	scheme, ok := doc.Components.SecuritySchemes["bearerAuth"]
	require.True(t, ok)
	assert.Equal(t, "http", scheme.Type)
	assert.Equal(t, "bearer", scheme.Scheme)
	assert.Equal(t, "JWT", scheme.BearerFormat)
	assert.Equal(t, []map[string][]string{{"bearerAuth": {}}}, doc.Security)

	public := map[string]bool{
		"GET /health":                true,
		"GET /api/v1/openapi.yaml":   true,
		"POST /api/v1/auth/register": true,
		"POST /api/v1/auth/login":    true,
	}
	protected := 0
	for path, item := range doc.Paths {
		for method, node := range item {
			if !openAPIMethods[method] {
				continue
			}
			operation := strings.ToUpper(method) + " " + path
			var op openAPIOperation
			require.NoError(t, node.Decode(&op), operation)

			if public[operation] {
				require.NotNil(t, op.Security, operation)
				assert.Empty(t, *op.Security, operation)
				continue
			}
			protected++
			assert.Nil(t, op.Security, operation)
			assert.Contains(t, op.Responses, "401", operation)
			assert.Contains(t, op.Responses, "403", operation)
			if strings.HasPrefix(path, "/api/v1/accounts/{") {
				assert.Contains(t, op.Responses, "404", operation)
			}
		}
	}
	assert.Equal(t, 13, protected)
}

func TestOpenAPIDocumentMakesTheAccountOwnerOptional(t *testing.T) {
	var doc openAPISecurityDocument
	require.NoError(t, yaml.Unmarshal(api.Spec, &doc))

	assert.NotContains(t, doc.Components.Schemas["CreateAccountRequest"].Required, "user_id")
	assert.Contains(t, doc.Components.Schemas["CreateAccountRequest"].Required, "account_type")
}

func TestOpenAPIDocumentsTheGatewayRejections(t *testing.T) {
	var doc openAPISecurityDocument
	require.NoError(t, yaml.Unmarshal(api.Spec, &doc))

	operation := func(path, method string) openAPIOperation {
		var op openAPIOperation
		node := doc.Paths[path][method]
		require.NoError(t, node.Decode(&op), method+" "+path)
		return op
	}
	reference := func(op openAPIOperation, status string) string {
		var response struct {
			Ref string `yaml:"$ref"`
		}
		node := op.Responses[status]
		require.NoError(t, node.Decode(&response), status)
		return response.Ref
	}

	for _, path := range []string{"/api/v1/auth/register", "/api/v1/auth/login"} {
		assert.Equal(t, "#/components/responses/ServiceBusy", reference(operation(path, "post"), "503"), path)
	}
	for _, path := range []string{"/api/v1/accounts/{id}", "/api/v1/accounts/{account_id}/transactions", "/api/v1/accounts/{account_id}/payments"} {
		assert.Contains(t, operation(path, "get").Responses, "422", path)
	}
}
