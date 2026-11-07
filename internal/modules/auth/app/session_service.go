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
		ResourceActions:   s.resourceActions(ctx, actor, namespace),
	}
}

func (s *SessionService) resourceActions(ctx context.Context, actor domain.Actor, namespace string) map[string]domain.ResourceCapabilities {
	actions := make(map[string]domain.ResourceCapabilities)
	add := func(resource, apiGroup string, namespaced bool, create, update, delete bool) {
		scope := ""
		if namespaced {
			scope = namespace
		}
		actions[resource] = domain.ResourceCapabilities{
			Create: create && s.allowed(ctx, actor, domain.Access{Verb: "create", APIGroup: apiGroup, Resource: resource, Namespace: scope}),
			Update: update && s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: apiGroup, Resource: resource, Namespace: scope}),
			Delete: delete && s.allowed(ctx, actor, domain.Access{Verb: "delete", APIGroup: apiGroup, Resource: resource, Namespace: scope}),
		}
	}
	add("pods", "", true, false, true, true)
	add("deployments", "apps", true, true, true, true)
	add("statefulsets", "apps", true, false, true, true)
	add("daemonsets", "apps", true, false, true, true)
	add("jobs", "batch", true, false, true, true)
	add("cronjobs", "batch", true, false, true, true)
	add("services", "", true, true, true, true)
	add("ingresses", "networking.k8s.io", true, true, true, true)
	add("configmaps", "", true, true, true, true)
	add("secrets", "", true, true, true, true)
	add("persistentvolumeclaims", "", true, true, true, true)
	add("persistentvolumes", "", false, false, false, false)
	add("storageclasses", "storage.k8s.io", false, false, false, false)
	add("nodes", "", false, false, true, false)
	add("namespaces", "", false, true, false, true)
	for _, resource := range []string{"deployments", "statefulsets"} {
		caps := actions[resource]
		caps.Scale = s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "apps", Resource: resource, Subresource: "scale", Namespace: namespace})
		actions[resource] = caps
	}
	return actions
}

func (s *SessionService) allowed(ctx context.Context, actor domain.Actor, access domain.Access) bool {
	return s.authorizer.Require(ctx, actor, access) == nil
}
