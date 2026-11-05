package certificateshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	certificatesapp "dosh/internal/modules/certificates/app"

	"github.com/go-chi/chi/v5"
)

type handlerRepository struct {
	status    domain.ServerCertificate
	reload    domain.ServerCertificate
	statusErr error
	reloadErr error
}

func (r *handlerRepository) Status(context.Context) (domain.ServerCertificate, error) {
	return r.status, r.statusErr
}
func (r *handlerRepository) Reload(context.Context) (domain.ServerCertificate, error) {
	return r.reload, r.reloadErr
}

type handlerAuthorizer struct{ err error }

func (a handlerAuthorizer) Require(context.Context, domain.Actor, domain.Access) error {
	return a.err
}

type handlerAuditor struct{}

func (handlerAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func certificateTestRouter(repo *handlerRepository, authorizer domain.Authorizer) http.Handler {
	service := certificatesapp.New(repo, authorizer, handlerAuditor{})
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(domain.WithActor(r.Context(), domain.Actor{Username: "admin", Admin: true})))
		})
	})
	New(service).Mount(router)
	return router
}

func certificateRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

func TestCertificateStatusNeverIncludesPrivateKeyMaterial(t *testing.T) {
	repo := &handlerRepository{status: domain.ServerCertificate{
		Mode: "https", Enabled: true, Configured: true, Status: "valid",
		DNSNames: []string{"dosh.example.test"}, IPAddresses: []string{"192.168.122.100"},
		FingerprintSHA256: strings.Repeat("A", 64), AutoReload: true, ReloadIntervalSeconds: 30,
	}}
	response := certificateRequest(certificateTestRouter(repo, handlerAuthorizer{}), http.MethodGet, "/certificate")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "valid" || payload["fingerprintSha256"] != strings.Repeat("A", 64) {
		t.Fatalf("payload = %#v", payload)
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"privatekey", "private_key", "tls.key", "-----begin"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response contains private key marker %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestCertificateReloadRequiresPermissionAndMapsErrors(t *testing.T) {
	repo := &handlerRepository{reload: domain.ServerCertificate{Mode: "https", Enabled: true, Status: "valid"}}
	router := certificateTestRouter(repo, handlerAuthorizer{err: domain.ErrForbidden})
	if response := certificateRequest(router, http.MethodPost, "/certificate/reload"); response.Code != http.StatusForbidden {
		t.Fatalf("denied reload status = %d, want 403", response.Code)
	}

	repo.reloadErr = domain.ErrInvalid
	router = certificateTestRouter(repo, handlerAuthorizer{})
	if response := certificateRequest(router, http.MethodPost, "/certificate/reload"); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid reload status = %d, want 400", response.Code)
	}

	repo.reloadErr = nil
	if response := certificateRequest(router, http.MethodPost, "/certificate/reload"); response.Code != http.StatusOK {
		t.Fatalf("reload status = %d, body = %s", response.Code, response.Body.String())
	}
}
