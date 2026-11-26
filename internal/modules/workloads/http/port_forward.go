package workloadshttp

import (
	"fmt"
	stdhttp "net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"dosh/internal/domain"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const maxTunnelRequestBody = 16 << 20

type portForwardRequest struct {
	Port  int32   `json:"port"`
	Ports []int32 `json:"ports,omitempty"`
	Pod   string  `json:"pod,omitempty"`
}

func (h *Handler) CreatePodTunnel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	h.createTunnel(w, r, "pod")
}

func (h *Handler) CreateServiceTunnel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	h.createTunnel(w, r, "service")
}

func (h *Handler) ListServiceTunnelTargets(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListPortForwardBackendPods(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createTunnel(w stdhttp.ResponseWriter, r *stdhttp.Request, kind string) {
	var request portForwardRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	session, err := h.service.OpenPortForward(r.Context(), actor, domain.PortForwardTarget{
		Kind: kind, Namespace: chi.URLParam(r, "namespace"), Name: chi.URLParam(r, "name"),
		Port: request.Port, Ports: request.Ports, PodName: request.Pod,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, session)
}

func (h *Handler) CloseTunnel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.ClosePortForward(r.Context(), actor, chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusNoContent, nil)
}

func (h *Handler) ListTunnels(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListPortForwards(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CloseAllTunnels(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	closed, err := h.service.CloseAllPortForwards(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]int{"closed": closed})
}

func (h *Handler) Tunnel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	requestedPort, portErr := parseTunnelPort(chi.URLParam(r, "port"))
	if portErr != nil {
		httpx.Error(w, r, portErr)
		return
	}
	endpoint, closer, err := h.service.GetPortForwardEndpoint(r.Context(), actor, chi.URLParam(r, "id"), requestedPort)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	target, err := url.Parse("http://" + endpoint.LocalAddress)
	if err != nil || target.Host == "" {
		httpx.Error(w, r, fmt.Errorf("%w: tunnel target is invalid", domain.ErrUnavailable))
		return
	}
	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxTunnelRequestBody)
	proxy := httputil.NewSingleHostReverseProxy(target)
	// Interactive tunnels must not buffer streaming bodies (SSE, chunked
	// downloads). WebSocket upgrades are handled by the standard library's
	// hijack path independently of this setting.
	proxy.FlushInterval = -1
	originalDirector := proxy.Director
	proxy.Director = func(request *stdhttp.Request) {
		originalDirector(request)
		rest := strings.TrimPrefix(chi.URLParam(request, "*"), "/")
		request.URL.Path = "/" + rest
		request.URL.RawPath = ""
		request.Host = target.Host
		request.Header.Set("X-Forwarded-Host", r.Host)
		request.Header.Set("X-Forwarded-Proto", requestScheme(r))
		request.Header.Set("X-Forwarded-Prefix", strings.TrimSuffix(endpoint.Path, "/"))
		request.Header.Set("Cookie", filterTunnelCookies(request.Header.Get("Cookie")))
	}
	proxy.ModifyResponse = func(response *stdhttp.Response) error {
		filterTunnelSetCookies(response.Header, endpoint.Path)
		location := response.Header.Get("Location")
		if location == "" {
			return nil
		}
		parsed, parseErr := url.Parse(location)
		if parseErr != nil || (parsed.Host != "" && !strings.EqualFold(parsed.Host, target.Host)) {
			return nil
		}
		response.Header.Set("Location", endpoint.Path+strings.TrimPrefix(parsed.EscapedPath(), "/")+querySuffix(parsed.RawQuery))
		return nil
	}
	proxy.ErrorHandler = func(writer stdhttp.ResponseWriter, request *stdhttp.Request, proxyErr error) {
		httpx.Error(writer, request, fmt.Errorf("%w: tunnel upstream request failed", domain.ErrUnavailable))
	}
	proxy.ServeHTTP(w, r)
}

// RedirectTunnel keeps the public tunnel URL usable when a browser strips the
// trailing slash. The proxy path itself requires the slash so DELETE on the
// root remains the explicit session-close operation.
func (h *Handler) RedirectTunnel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	requestedPort, portErr := parseTunnelPort(chi.URLParam(r, "port"))
	if portErr != nil {
		httpx.Error(w, r, portErr)
		return
	}
	endpoint, err := h.service.GetPortForwardRoute(r.Context(), actor, chi.URLParam(r, "id"), requestedPort)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	stdhttp.Redirect(w, r, endpoint.Path, stdhttp.StatusTemporaryRedirect)
}

func parseTunnelPort(raw string) (int32, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 1 || value > 65535 {
		return 0, fmt.Errorf("%w: tunnel port must be between 1 and 65535", domain.ErrInvalid)
	}
	return int32(value), nil
}

func requestScheme(r *stdhttp.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded != "" {
		return forwarded
	}
	return "http"
}

func filterTunnelCookies(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ";")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(strings.SplitN(part, "=", 2)[0])
		if isDoshCookie(name) {
			continue
		}
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, "; ")
}

// filterTunnelSetCookies preserves cookies owned by the target application but
// scopes them to the public tunnel prefix and never lets them overwrite or
// clear Dosh's console session. Invalid Set-Cookie values are dropped so they
// cannot bypass the name check with parser differences.
func filterTunnelSetCookies(header stdhttp.Header, pathPrefix string) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	pathPrefix = "/" + strings.Trim(strings.TrimSpace(pathPrefix), "/")
	if pathPrefix == "/" {
		pathPrefix = ""
	}
	kept := make([]string, 0, len(values))
	for _, value := range values {
		cookie, err := stdhttp.ParseSetCookie(value)
		if err != nil || isDoshCookie(cookie.Name) {
			continue
		}
		cookie.Domain = ""
		if pathPrefix != "" {
			targetPath := strings.TrimSpace(cookie.Path)
			if targetPath == "" || targetPath == "/" {
				cookie.Path = pathPrefix + "/"
			} else if !strings.HasPrefix(targetPath, "/") {
				cookie.Path = pathPrefix + "/" + targetPath
			} else {
				cookie.Path = pathPrefix + targetPath
			}
		}
		kept = append(kept, cookie.String())
	}
	header.Del("Set-Cookie")
	for _, value := range kept {
		header.Add("Set-Cookie", value)
	}
}

func isDoshCookie(name string) bool {
	return name == "dosh_session" || name == "dosh_oidc_state"
}

func querySuffix(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	return "?" + rawQuery
}
