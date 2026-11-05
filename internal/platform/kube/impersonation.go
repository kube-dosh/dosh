package kube

import (
	"net/http"

	"dosh/internal/domain"
)

type impersonationTransport struct {
	base http.RoundTripper
}

func (t *impersonationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	actor, ok := domain.ActorFromContext(request.Context())
	if !ok || actor.Username == "" {
		return t.base.RoundTrip(request)
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Impersonate-User", actor.Username)
	for _, group := range actor.Groups {
		if group != "" {
			clone.Header.Add("Impersonate-Group", group)
		}
	}
	return t.base.RoundTrip(clone)
}
