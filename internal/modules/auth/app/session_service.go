package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		NodeDrain:         s.allowed(ctx, actor, domain.Access{Verb: "update", Resource: "nodes"}),
		PodExec:           s.allowed(ctx, actor, domain.Access{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: namespace}),
		PodEphemeralDebug: s.allowed(ctx, actor, domain.Access{Verb: "update", Resource: "pods", Subresource: "ephemeralcontainers", Namespace: namespace}),
		PodPortForward:    s.allowed(ctx, actor, domain.Access{Verb: "create", Resource: "pods", Subresource: "portforward", Namespace: namespace}),
		HelmReleases:      s.allowed(ctx, actor, domain.Access{Verb: "list", Resource: "secrets", Namespace: namespace}),
		HelmUpgrade:       s.allowed(ctx, actor, domain.Access{Verb: "create", Resource: "secrets", Namespace: namespace}) && s.allowed(ctx, actor, domain.Access{Verb: "update", Resource: "secrets", Namespace: namespace}),
		ReloadCertificate: s.allowed(ctx, actor, domain.Access{Verb: "update", APIGroup: "dosh.io", Resource: "server-certificates"}),
		ResourceActions:   s.resourceActions(ctx, actor, namespace),
	}
}

// Review returns a human-readable authorization decision for the current
// actor. The SubjectAccessReview remains the source of truth; this method only
// adds guidance about Dosh read-only mode versus Kubernetes RBAC denial.
func (s *SessionService) Review(ctx context.Context, actor domain.Actor, access domain.Access) domain.AccessReview {
	review := domain.AccessReview{
		Verb: access.Verb, APIGroup: access.APIGroup, Resource: access.Resource,
		Subresource: access.Subresource, Namespace: access.Namespace, Name: access.Name,
	}
	err := s.authorizer.Require(ctx, actor, access)
	review.Allowed = err == nil
	resource := access.Resource
	if access.APIGroup != "" {
		resource += "." + access.APIGroup
	}
	if access.Subresource != "" {
		resource += "/" + access.Subresource
	}
	command := fmt.Sprintf("kubectl auth can-i %s %s", access.Verb, resource)
	if access.Namespace != "" {
		command += " --namespace " + access.Namespace
	}
	if access.Name != "" {
		command += " --resource-name " + access.Name
	}
	review.KubectlCommand = command
	switch {
	case err == nil:
		review.Reason = "allowed"
		review.Guidance = "当前身份拥有该操作权限。若页面仍返回 403，请检查请求的命名空间、资源名称或 Subresource 是否与检查条件一致。"
	case errors.Is(err, domain.ErrReadOnly):
		review.Reason = "read_only"
		review.Guidance = "Dosh 当前运行在只读模式，写操作被本地策略阻止。请联系管理员确认服务启动配置，而不是只调整 Kubernetes RBAC。"
	case errors.Is(err, domain.ErrForbidden):
		review.Reason = "rbac_denied"
		review.Guidance = "Kubernetes RBAC 未授权当前身份执行该操作。请管理员为对应 Role/ClusterRole 增加该 apiGroup、resource、subresource 和 verb，并确认 RoleBinding 覆盖目标命名空间。"
	default:
		review.Reason = "authorization_unavailable"
		review.Guidance = "授权检查暂时不可用，无法确认是否允许。请检查 Dosh 到 Kubernetes API Server 的连通性和 SubjectAccessReview 权限。"
	}
	if strings.TrimSpace(actor.Username) == "" {
		review.Guidance = "当前请求没有有效登录身份，请重新登录后检查权限。"
	}
	return review
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
	add("horizontalpodautoscalers", "autoscaling", true, true, true, true)
	add("poddisruptionbudgets", "policy", true, true, true, true)
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
