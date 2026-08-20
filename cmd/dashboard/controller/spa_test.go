package controller

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	openapispec "github.com/hi2shark/santaizi-dashboard/openapi"
	"github.com/hi2shark/santaizi-dashboard/resource"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gopkg.in/yaml.v3"
)

func TestServeWebRegistersSPAAndV2Routes(t *testing.T) {
	original := singleton.Conf
	defer func() { singleton.Conf = original }()
	singleton.Conf = &model.Config{Site: model.SiteConfig{CookieName: "santaizi", Theme: "server-status", DashboardTheme: "spa"}, Web: model.WebConfig{Delivery: "embedded"}}
	server := ServeWeb(0)
	engine, ok := server.Handler.(*gin.Engine)
	if !ok {
		t.Fatal("HTTP handler is not a Gin engine")
	}
	want := map[string]bool{
		"GET /": false, "GET /assets/*filepath": false, "GET /server/:serverId": false,
		"GET /admin/*path": false, "GET /docs/api/*path": false,
		"GET /openapi/v2.yaml": false, "GET /api/v2/auth/session": false,
		"GET /api/v2/admin/servers": false, "POST /api/v2/admin/telemetry/collectors": false,
		"GET /ws/v2/public/runtime": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("route %s is not registered", route)
		}
	}
	for _, route := range engine.Routes() {
		if strings.Contains(route.Path, "terminal") || strings.Contains(route.Path, "file-sessions") || strings.Contains(route.Path, "/tasks") {
			t.Errorf("removed remote capability route remains registered: %s %s", route.Method, route.Path)
		}
	}
}

func TestV2SessionIncludesVersion(t *testing.T) {
	original := singleton.Conf
	defer func() { singleton.Conf = original }()
	singleton.Conf = &model.Config{Site: model.SiteConfig{CookieName: "santaizi"}, Web: model.WebConfig{Delivery: "embedded"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v2/auth/session", nil)
	response := httptest.NewRecorder()
	ServeWeb(0).Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"version":"`+singleton.Version+`"`) {
		t.Fatalf("session missing version %q: %s", singleton.Version, response.Body.String())
	}
}

func TestV2UnauthorizedUsesProblemDetails(t *testing.T) {
	original := singleton.Conf
	defer func() { singleton.Conf = original }()
	singleton.Conf = &model.Config{Site: model.SiteConfig{CookieName: "santaizi"}, Web: model.WebConfig{Delivery: "embedded"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v2/admin/summary", nil)
	response := httptest.NewRecorder()
	ServeWeb(0).Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/problem+json") {
		t.Fatalf("unexpected content type %q", contentType)
	}
	for _, field := range []string{`"code":"authentication_required"`, `"trace_id"`, `"status":401`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Errorf("problem response is missing %s: %s", field, response.Body.String())
		}
	}
}

func TestSafeAppearanceValidation(t *testing.T) {
	for _, unsafe := range []string{
		`@import "https://example.com/theme.css";`,
		`body { background: url(//example.com/a.png) }`,
		`a { width: expression(alert(1)) }`,
		`</style><script>alert(1)</script>`,
	} {
		if !forbiddenCSS.MatchString(unsafe) {
			t.Errorf("unsafe CSS was accepted: %s", unsafe)
		}
	}
	if forbiddenCSS.MatchString(`:root { --brand: #2563eb } .status-panel { border-radius: 12px }`) {
		t.Fatal("safe CSS was rejected")
	}
	if got := safeAssetURL("https://example.com/logo.svg", "/static/logo.svg"); got != "/static/logo.svg" {
		t.Fatalf("external asset URL was accepted: %q", got)
	}
}

func TestOpenAPIV2Contract(t *testing.T) {
	var document struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openapispec.V2YAML, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.0.3" {
		t.Fatalf("unexpected OpenAPI version %q", document.OpenAPI)
	}
	for _, path := range []string{"/api/v2/auth/session", "/api/v2/public/servers", "/api/v2/admin/servers", "/api/v2/admin/telemetry/collectors"} {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("OpenAPI path %s is missing", path)
		}
	}
	original := singleton.Conf
	defer func() { singleton.Conf = original }()
	singleton.Conf = &model.Config{Site: model.SiteConfig{CookieName: "santaizi"}, Web: model.WebConfig{Delivery: "embedded"}}
	engine := ServeWeb(0).Handler.(*gin.Engine)
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+normalizeContractPath(route.Path)] = true
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for routePath, operations := range document.Paths {
		for method := range operations {
			if !methods[strings.ToLower(method)] {
				continue
			}
			key := strings.ToUpper(method) + " " + normalizeContractPath(routePath)
			if !registered[key] {
				t.Errorf("OpenAPI operation %s is not registered", key)
			}
		}
	}
}

var contractParameter = regexp.MustCompile(`\{[^}]+\}|:[^/]+`)

func normalizeContractPath(value string) string {
	return contractParameter.ReplaceAllString(value, "{}")
}

func withEmbeddedWeb(t *testing.T) http.Handler {
	t.Helper()
	original := singleton.Conf
	t.Cleanup(func() { singleton.Conf = original })
	singleton.Conf = &model.Config{Site: model.SiteConfig{CookieName: "santaizi", Theme: "server-status", DashboardTheme: "spa"}, Web: model.WebConfig{Delivery: "embedded"}}
	return ServeWeb(0).Handler
}

func TestPublicStatusRoutesServeSPANotAdminRedirect(t *testing.T) {
	handler := withEmbeddedWeb(t)

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/server", nil))
	if legacy.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET /server status = %d, want 308", legacy.Code)
	}
	if loc := legacy.Header().Get("Location"); loc != "/admin/servers" {
		t.Fatalf("GET /server Location = %q, want /admin/servers", loc)
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/server/2", nil))
	if detail.Code == http.StatusPermanentRedirect || detail.Code == http.StatusMovedPermanently {
		t.Fatalf("GET /server/2 redirected to %q", detail.Header().Get("Location"))
	}
	if detail.Code == http.StatusNotFound {
		t.Fatalf("GET /server/2 returned 404: %s", detail.Body.String())
	}

	missingAsset := httptest.NewRecorder()
	handler.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/assets/missing-theme.js", nil))
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missingAsset.Code)
	}
	if strings.Contains(missingAsset.Header().Get("Content-Type"), "text/html") {
		t.Fatal("missing asset must not fall back to index.html")
	}
}

func TestEmbeddedStatusAssetsAreServedWhenBuilt(t *testing.T) {
	if _, err := fs.Stat(resource.WebFS, "web/status/index.html"); err != nil {
		t.Skip("status SPA is not embedded; run pnpm build before go test")
	}
	handler := withEmbeddedWeb(t)

	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if home.Code != http.StatusOK {
		t.Fatalf("GET / status = %d: %s", home.Code, home.Body.String())
	}
	html := home.Body.String()
	if !strings.Contains(html, "<div id=\"app\">") {
		t.Fatalf("GET / did not return status index.html: %s", html)
	}
	if !strings.Contains(html, `rel="icon"`) || !strings.Contains(html, `href="/static/logo.svg"`) {
		t.Fatal("status index.html missing favicon link to /static/logo.svg")
	}

	assetRefs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(html, -1)
	if len(assetRefs) == 0 {
		t.Fatal("status index.html has no /assets/ references")
	}
	for _, ref := range assetRefs {
		asset := httptest.NewRecorder()
		handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, ref[1], nil))
		if asset.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d: %s", ref[1], asset.Code, asset.Body.String())
		}
		contentType := asset.Header().Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			t.Fatalf("GET %s returned HTML instead of the hashed asset", ref[1])
		}
	}

	nazhuaMap := httptest.NewRecorder()
	handler.ServeHTTP(nazhuaMap, httptest.NewRequest(http.MethodGet, "/static/theme-nazhua/maps/world.geo.json", nil))
	if nazhuaMap.Code != http.StatusOK {
		t.Fatalf("GET nazhua world.geo.json status = %d: %s", nazhuaMap.Code, nazhuaMap.Body.String())
	}

	logo := httptest.NewRecorder()
	handler.ServeHTTP(logo, httptest.NewRequest(http.MethodGet, "/static/logo.svg", nil))
	if logo.Code != http.StatusOK {
		t.Fatalf("GET /static/logo.svg status = %d: %s", logo.Code, logo.Body.String())
	}
	if !strings.Contains(logo.Header().Get("Content-Type"), "svg") && !strings.Contains(logo.Body.String(), "<svg") {
		t.Fatalf("GET /static/logo.svg is not an SVG: %s", logo.Header().Get("Content-Type"))
	}
}
