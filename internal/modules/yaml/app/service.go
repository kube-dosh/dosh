package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"dosh/internal/domain"

	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"
)

type Repository interface {
	GetYAML(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error)
	ApplyYAML(ctx context.Context, request domain.ApplyRequest) (domain.YAMLResource, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) Get(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error) {
	return s.repository.GetYAML(ctx, resourceType, namespace, name)
}

func (s *Service) Apply(ctx context.Context, actor domain.Actor, request domain.ApplyRequest) (domain.YAMLResource, error) {
	access, err := accessForYAML(request.YAML)
	if err != nil {
		return domain.YAMLResource{}, err
	}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "apply", Resource: access.Resource, Namespace: access.Namespace, Name: access.Name, Result: "denied", Err: err})
		return domain.YAMLResource{}, err
	}
	if !request.DryRun {
		dryRun := request
		dryRun.DryRun = true
		if _, err := s.repository.ApplyYAML(ctx, dryRun); err != nil {
			return domain.YAMLResource{}, err
		}
	}
	result, err := s.repository.ApplyYAML(ctx, request)
	action := "apply"
	if request.DryRun {
		action = "dry-run"
	}
	auditResult := "success"
	if err != nil {
		auditResult = "failure"
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "yaml", Name: result.Name, Namespace: result.Namespace, Result: auditResult, Err: err})
	return result, err
}

type yamlIdentity struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

func accessForYAML(body string) (domain.Access, error) {
	if err := validateSingleYAMLDocument(body); err != nil {
		return domain.Access{}, err
	}
	var identity yamlIdentity
	if err := yaml.Unmarshal([]byte(body), &identity); err != nil {
		return domain.Access{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if identity.Metadata.Name == "" {
		return domain.Access{}, fmt.Errorf("%w: metadata.name is required", domain.ErrInvalid)
	}
	access := domain.Access{Verb: "patch", Namespace: identity.Metadata.Namespace, Name: identity.Metadata.Name}
	namespaced := true
	switch identity.APIVersion + "/" + identity.Kind {
	case "v1/Pod":
		access.Resource = "pods"
	case "apps/v1/Deployment":
		access.APIGroup, access.Resource = "apps", "deployments"
	case "apps/v1/StatefulSet":
		access.APIGroup, access.Resource = "apps", "statefulsets"
	case "apps/v1/DaemonSet":
		access.APIGroup, access.Resource = "apps", "daemonsets"
	case "batch/v1/Job":
		access.APIGroup, access.Resource = "batch", "jobs"
	case "batch/v1/CronJob":
		access.APIGroup, access.Resource = "batch", "cronjobs"
	case "v1/Service":
		access.Resource = "services"
	case "networking.k8s.io/v1/Ingress":
		access.APIGroup, access.Resource = "networking.k8s.io", "ingresses"
	case "v1/ConfigMap":
		access.Resource = "configmaps"
	case "v1/Secret":
		access.Resource = "secrets"
	case "storage.k8s.io/v1/StorageClass":
		access.APIGroup, access.Resource, namespaced = "storage.k8s.io", "storageclasses", false
	case "v1/PersistentVolume":
		access.Resource, namespaced = "persistentvolumes", false
	case "v1/PersistentVolumeClaim":
		access.Resource = "persistentvolumeclaims"
	default:
		return domain.Access{}, fmt.Errorf("%w: resource %s/%s is not editable", domain.ErrUnsupported, identity.APIVersion, identity.Kind)
	}
	if namespaced && access.Namespace == "" {
		return domain.Access{}, fmt.Errorf("%w: metadata.namespace is required", domain.ErrInvalid)
	}
	return access, nil
}

func validateSingleYAMLDocument(body string) error {
	decoder := yamlv3.NewDecoder(strings.NewReader(body))
	var document any
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if document == nil {
		return fmt.Errorf("%w: yaml document is required", domain.ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: exactly one yaml document is required", domain.ErrInvalid)
		}
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return nil
}
