package app

import (
	"context"
	"time"

	"dosh/internal/domain"
)

type SessionService struct {
	manager    *SessionManager
	authorizer domain.Authorizer
	authMode   string
}

func NewSessionService(manager *SessionManager, authorizer domain.Authorizer, authMode string) *SessionService {
	return &SessionService{manager: manager, authorizer: authorizer, authMode: authMode}
}

func (s *SessionService) Manager() *SessionManager { return s.manager }

func (s *SessionService) New(ctx context.Context, actor domain.Actor, expiresAt time.Time) (Session, error) {
	return s.manager.NewWithExpiry(actor.Username, actor.Groups, actor.Admin, expiresAt)
}

func (s *SessionService) Response(ctx context.Context, session Session) SessionResponse {
	actor := domain.Actor{Username: session.Username, Groups: session.Groups, Admin: session.Admin}
	return SessionResponse{
		User: actor, CSRFToken: session.CSRFToken, ExpiresAt: session.ExpiresAt,
		Capabilities: s.Capabilities(ctx, actor, ""), AuthMode: s.authMode,
	}
}

func (s *SessionService) Capabilities(ctx context.Context, actor domain.Actor, namespace string) domain.Capabilities {
	if actor.Username == "" {
		return domain.Capabilities{}
	}
	return domain.Capabilities{
		Read:              s.allowed(ctx, actor, domain.Access{Verb: "list", Resource: "pods", Namespace: namespace}),
		Scale:             s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "apps", Resource: "deployments", Subresource: "scale", Namespace: namespace}),
		Restart:           s.allowed(ctx, actor, domain.Access{Verb: "patch", APIGroup: "apps", Resource: "deployments", Namespace: namespace}),
		UpdateImage:       s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "apps", Resource: "deployments", Namespace: namespace}),
		SuspendCronJob:    s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "batch", Resource: "cronjobs", Namespace: namespace}),
		Delete:            s.allowed(ctx, actor, domain.Access{Verb: "delete", APIGroup: "apps", Resource: "deployments", Namespace: namespace}),
		Create:            s.allowed(ctx, actor, domain.Access{Verb: "create", APIGroup: "apps", Resource: "deployments", Namespace: namespace}),
		CreateService:     s.allowed(ctx, actor, domain.Access{Verb: "create", Resource: "services", Namespace: namespace}),
		UpdateService:     s.allowed(ctx, actor, domain.Access{Verb: "update", Resource: "services", Namespace: namespace}),
		DeleteService:     s.allowed(ctx, actor, domain.Access{Verb: "delete", Resource: "services", Namespace: namespace}),
		DeleteNamespace:   s.allowed(ctx, actor, domain.Access{Verb: "delete", Resource: "namespaces"}),
		RevealSecrets:     s.allowed(ctx, actor, domain.Access{Verb: "get", Resource: "secrets", Namespace: namespace}),
		EditYAML:          s.allowed(ctx, actor, domain.Access{Verb: "patch", Resource: "configmaps", Namespace: namespace}),
		NodeCordon:        s.allowed(ctx, actor, domain.Access{Verb: "update", Resource: "nodes"}),
		ReloadCertificate: s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "dosh.io", Resource: "server-certificates"}),
	}
}

func (s *SessionService) allowed(ctx context.Context, actor domain.Actor, access domain.Access) bool {
	return s.authorizer.Require(ctx, actor, access) == nil
}
