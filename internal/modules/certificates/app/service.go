package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	Status(ctx context.Context) (domain.ServerCertificate, error)
	Reload(ctx context.Context) (domain.ServerCertificate, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) Status(ctx context.Context) (domain.ServerCertificate, error) {
	if s.repository == nil {
		return domain.ServerCertificate{Mode: "http", Status: "disabled", DNSNames: []string{}, IPAddresses: []string{}}, nil
	}
	return s.repository.Status(ctx)
}

func (s *Service) Reload(ctx context.Context, actor domain.Actor) (domain.ServerCertificate, error) {
	access := domain.Access{Verb: "update", APIGroup: "dosh.io", Resource: "server-certificates"}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "reload", Resource: "server-certificate", Result: "denied", Err: err})
		return domain.ServerCertificate{}, err
	}
	if s.repository == nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "reload", Resource: "server-certificate", Result: "failure", Err: domain.ErrUnsupported})
		return domain.ServerCertificate{}, domain.ErrUnsupported
	}
	status, err := s.repository.Reload(ctx)
	result := "success"
	if err != nil {
		result = "failure"
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "reload", Resource: "server-certificate", Result: result, Err: err})
	return status, err
}
