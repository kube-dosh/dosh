package authhttp

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	stdhttp "net/http"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type sessionContextKey struct{}

type Options struct {
	Mode         string
	ProviderName string
	Local        *authapp.Service
	Token        *authapp.KubernetesTokenService
	OIDC         *authapp.OIDCService
	Sessions     *authapp.SessionService
	Audit        *audit.Logger
	Logger       *slog.Logger
	CookieSecure bool
}

type Handler struct {
	mode         string
	providerName string
	local        *authapp.Service
	token        *authapp.KubernetesTokenService
	oidc         *authapp.OIDCService
	sessions     *authapp.SessionService
	audit        *audit.Logger
	logger       *slog.Logger
	cookieSecure bool
}

func New(options Options) *Handler {
	mode := options.Mode
	if mode == "" {
		mode = "local"
	}
	return &Handler{
		mode: mode, providerName: options.ProviderName, local: options.Local, token: options.Token, oidc: options.OIDC,
		sessions: options.Sessions, audit: options.Audit, logger: options.Logger, cookieSecure: options.CookieSecure,
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type authConfigResponse struct {
	Mode         string `json:"mode"`
	ProviderName string `json:"providerName,omitempty"`
}

func (h *Handler) AuthConfig(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, authConfigResponse{Mode: h.mode, ProviderName: h.providerName})
}

func (h *Handler) Login(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h.mode == "token" {
		h.loginWithToken(w, r)
		return
	}
	if h.local == nil {
		httpx.Error(w, r, domain.ErrUnsupported)
		return
	}
	var request loginRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Username == "" || request.Password == "" {
		httpx.Error(w, r, httpx.Validation("credentials", "username and password are required"))
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	session, err := h.local.LoginContext(r.Context(), ip, request.Username, request.Password)
	if err != nil {
		h.audit.Record(r.Context(), domainActor(request.Username), domain.AuditEvent{Action: "login", Resource: "session", Result: "denied", Err: err})
		httpx.Error(w, r, err)
		return
	}
	value, err := h.sessions.Manager().Encode(session)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.setSessionCookie(w, value, session.ExpiresAt)
	h.audit.Record(r.Context(), actorFromSession(session), domain.AuditEvent{Action: "login", Resource: "session", Result: "success"})
	httpx.JSON(w, stdhttp.StatusOK, h.sessions.Response(r.Context(), session))
}

func (h *Handler) loginWithToken(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h.token == nil {
		httpx.Error(w, r, domain.ErrUnsupported)
		return
	}
	var request loginRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Token == "" {
		httpx.Error(w, r, httpx.Validation("token", "Kubernetes token is required"))
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	session, err := h.token.LoginContext(r.Context(), ip, request.Token)
	if err != nil {
		h.audit.Record(r.Context(), domain.Actor{}, domain.AuditEvent{Action: "login", Resource: "session", Result: "denied", Err: err})
		httpx.Error(w, r, err)
		return
	}
	value, err := h.sessions.Manager().Encode(session)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.setSessionCookie(w, value, session.ExpiresAt)
	h.audit.Record(r.Context(), actorFromSession(session), domain.AuditEvent{Action: "login", Resource: "session", Result: "success"})
	httpx.JSON(w, stdhttp.StatusOK, h.sessions.Response(r.Context(), session))
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	session, ok := SessionFrom(r.Context())
	if !ok {
		httpx.Error(w, r, domain.ErrUnauthorized)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, h.sessions.Response(r.Context(), session))
}

func (h *Handler) Logout(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	session, _ := SessionFrom(r.Context())
	h.clearSessionCookie(w)
	h.audit.Record(r.Context(), actorFromSession(session), domain.AuditEvent{Action: "logout", Resource: "session", Result: "success"})
	httpx.JSON(w, stdhttp.StatusNoContent, nil)
}

func (h *Handler) Capabilities(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	httpx.JSON(w, stdhttp.StatusOK, h.sessions.Capabilities(r.Context(), actor, httpx.Query(r, "namespace")))
}

func (h *Handler) OIDCLogin(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h.oidc == nil {
		httpx.Error(w, r, domain.ErrNotFound)
		return
	}
	authorizationURL, state, expiresAt, err := h.oidc.Begin(httpx.Query(r, "returnTo"))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "oidc_login_start_failed", "error", err)
		httpx.Error(w, r, domain.ErrUnavailable)
		return
	}
	h.setOIDCStateCookie(w, state, expiresAt)
	h.audit.Record(r.Context(), domain.Actor{}, domain.AuditEvent{Action: "login_started", Resource: "session", Result: "success"})
	stdhttp.Redirect(w, r, authorizationURL, stdhttp.StatusFound)
}

func (h *Handler) OIDCCallback(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h.oidc == nil {
		httpx.Error(w, r, domain.ErrNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	stateCookie, err := r.Cookie(authapp.OIDCStateCookieName)
	if err != nil || stateCookie.Value == "" {
		h.oidcCallbackFailure(w, r, domain.ErrUnauthorized)
		return
	}
	if providerError := httpx.Query(r, "error"); providerError != "" {
		h.oidcCallbackFailure(w, r, domain.ErrUnauthorized)
		return
	}
	returnTo := h.oidc.ReturnTo(stateCookie.Value)
	session, err := h.oidc.Complete(r.Context(), httpx.Query(r, "code"), httpx.Query(r, "state"), stateCookie.Value)
	h.clearOIDCStateCookie(w)
	if err != nil {
		h.oidcCallbackFailure(w, r, err)
		return
	}
	value, err := h.sessions.Manager().Encode(session)
	if err != nil {
		h.oidcCallbackFailure(w, r, err)
		return
	}
	h.setSessionCookie(w, value, session.ExpiresAt)
	h.audit.Record(r.Context(), actorFromSession(session), domain.AuditEvent{Action: "login", Resource: "session", Result: "success", Name: session.Username})
	stdhttp.Redirect(w, r, returnTo, stdhttp.StatusFound)
}

func (h *Handler) OIDCLogout(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h.oidc == nil {
		httpx.Error(w, r, domain.ErrNotFound)
		return
	}
	session, _ := h.sessionFromRequest(r)
	h.clearSessionCookie(w)
	h.audit.Record(r.Context(), actorFromSession(session), domain.AuditEvent{Action: "logout", Resource: "session", Result: "success"})
	if target := h.oidc.LogoutURL(); target != "" {
		stdhttp.Redirect(w, r, target, stdhttp.StatusFound)
		return
	}
	stdhttp.Redirect(w, r, "/login", stdhttp.StatusFound)
}

func (h *Handler) oidcCallbackFailure(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	h.logger.ErrorContext(r.Context(), "oidc_callback_failed", "error", err)
	h.audit.Record(r.Context(), domain.Actor{}, domain.AuditEvent{Action: "login", Resource: "session", Result: "denied", Err: err})
	h.clearOIDCStateCookie(w)
	stdhttp.Redirect(w, r, "/login?error=oidc", stdhttp.StatusFound)
}

func (h *Handler) RequireSession(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		session, err := h.sessionFromRequest(r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
		ctx = domain.WithActor(ctx, actorFromSession(session))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) sessionFromRequest(r *stdhttp.Request) (authapp.Session, error) {
	cookie, err := r.Cookie(authapp.CookieName)
	if err != nil {
		return authapp.Session{}, domain.ErrUnauthorized
	}
	return h.sessions.Manager().Decode(cookie.Value)
}

func (h *Handler) setSessionCookie(w stdhttp.ResponseWriter, value string, expiresAt time.Time) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{
		Name: authapp.CookieName, Value: value, Path: "/", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: stdhttp.SameSiteStrictMode, Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds()),
	})
}

func (h *Handler) clearSessionCookie(w stdhttp.ResponseWriter) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{
		Name: authapp.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: stdhttp.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func (h *Handler) setOIDCStateCookie(w stdhttp.ResponseWriter, value string, expiresAt time.Time) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{
		Name: authapp.OIDCStateCookieName, Value: value, Path: "/api/v1/auth/oidc", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: stdhttp.SameSiteLaxMode, Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds()),
	})
}

func (h *Handler) clearOIDCStateCookie(w stdhttp.ResponseWriter) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{
		Name: authapp.OIDCStateCookieName, Value: "", Path: "/api/v1/auth/oidc", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: stdhttp.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func (h *Handler) RequireCSRF(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.Method {
		case stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		session, ok := SessionFrom(r.Context())
		if !ok {
			httpx.Error(w, r, domain.ErrUnauthorized)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) != 1 {
			httpx.Error(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SessionFrom(ctx context.Context) (authapp.Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(authapp.Session)
	return session, ok
}

func MountPublic(r chi.Router, handler *Handler) {
	r.Get("/auth/config", handler.AuthConfig)
	if handler.local != nil || handler.token != nil {
		r.Post("/session", handler.Login)
	}
	if handler.oidc != nil {
		r.Get("/auth/oidc/login", handler.OIDCLogin)
		r.Get("/auth/oidc/callback", handler.OIDCCallback)
	}
}

func MountProtected(r chi.Router, handler *Handler) {
	r.Get("/session", handler.Get)
	r.Delete("/session", handler.Logout)
	r.Get("/capabilities", handler.Capabilities)
	if handler.oidc != nil {
		r.Get("/auth/oidc/logout", handler.OIDCLogout)
	}
}

func domainActor(username string) domain.Actor {
	return domain.Actor{Username: username}
}

func actorFromSession(session authapp.Session) domain.Actor {
	return domain.Actor{Username: session.Username, Groups: session.Groups, Admin: session.Admin}
}
