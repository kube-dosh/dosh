package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"dosh/internal/domain"

	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"
)

type Repository interface {
	GetYAML(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error)
	Exists(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (bool, error)
	ApplyYAML(ctx context.Context, request domain.ApplyRequest) (domain.YAMLResource, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

const maxYAMLBytes = 1024 * 1024

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) Get(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error) {
	return s.repository.GetYAML(ctx, resourceType, namespace, name)
}

func (s *Service) Apply(ctx context.Context, actor domain.Actor, request domain.ApplyRequest) (domain.YAMLResource, error) {
	target, err := targetForYAML(request.YAML)
	if err != nil {
		return domain.YAMLResource{}, err
	}
	action := "apply"
	if request.DryRun {
		action = "dry-run"
	}
	exists, err := s.repository.Exists(ctx, target.ResourceType, target.Access.Namespace, target.Access.Name)
	if err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: target.Access.Resource, Namespace: target.Access.Namespace, Name: target.Access.Name, Result: "failure", Err: err})
		return domain.YAMLResource{}, err
	}
	access, err := s.authorizeWrite(ctx, actor, target.Access, exists)
	if err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: target.Access.Resource, Namespace: target.Access.Namespace, Name: target.Access.Name, Result: "denied", Err: err})
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
	auditResult := "success"
	if err != nil {
		auditResult = "failure"
	}
	name := result.Name
	if name == "" {
		name = access.Name
	}
	namespace := result.Namespace
	if namespace == "" {
		namespace = access.Namespace
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "yaml", Name: name, Namespace: namespace, Result: auditResult, Err: err})
	return result, err
}

func (s *Service) authorizeWrite(ctx context.Context, actor domain.Actor, base domain.Access, exists bool) (domain.Access, error) {
	access := base
	if !exists {
		access.Verb = "create"
		return access, s.authorizer.Require(ctx, actor, access)
	}
	access.Verb = "update"
	if err := s.authorizer.Require(ctx, actor, access); err == nil {
		return access, nil
	} else if !errors.Is(err, domain.ErrForbidden) {
		return access, err
	}
	access.Verb = "patch"
	return access, s.authorizer.Require(ctx, actor, access)
}

type yamlIdentity struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

type yamlTarget struct {
	Access       domain.Access
	ResourceType domain.ResourceType
}

func targetForYAML(body string) (yamlTarget, error) {
	if err := validateSingleYAMLDocument(body); err != nil {
		return yamlTarget{}, err
	}
	var identity yamlIdentity
	if err := yaml.Unmarshal([]byte(body), &identity); err != nil {
		return yamlTarget{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if identity.Metadata.Name == "" {
		return yamlTarget{}, fmt.Errorf("%w: metadata.name is required", domain.ErrInvalid)
	}
	target := yamlTarget{Access: domain.Access{Namespace: identity.Metadata.Namespace, Name: identity.Metadata.Name}}
	namespaced := true
	switch identity.APIVersion + "/" + identity.Kind {
	case "v1/Pod":
		target.Access.Resource, target.ResourceType = "pods", domain.ResourcePod
	case "apps/v1/Deployment":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "apps", "deployments", domain.ResourceDeployment
	case "apps/v1/StatefulSet":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "apps", "statefulsets", domain.ResourceStatefulSet
	case "apps/v1/DaemonSet":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "apps", "daemonsets", domain.ResourceDaemonSet
	case "batch/v1/Job":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "batch", "jobs", domain.ResourceJob
	case "batch/v1/CronJob":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "batch", "cronjobs", domain.ResourceCronJob
	case "v1/Service":
		target.Access.Resource, target.ResourceType = "services", domain.ResourceService
	case "networking.k8s.io/v1/Ingress":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType = "networking.k8s.io", "ingresses", domain.ResourceIngress
	case "v1/ConfigMap":
		target.Access.Resource, target.ResourceType = "configmaps", domain.ResourceConfigMap
	case "v1/Secret":
		target.Access.Resource, target.ResourceType = "secrets", domain.ResourceSecret
	case "storage.k8s.io/v1/StorageClass":
		target.Access.APIGroup, target.Access.Resource, target.ResourceType, namespaced = "storage.k8s.io", "storageclasses", domain.ResourceStorageClass, false
	case "v1/PersistentVolume":
		target.Access.Resource, target.ResourceType, namespaced = "persistentvolumes", domain.ResourcePersistentVolume, false
	case "v1/PersistentVolumeClaim":
		target.Access.Resource, target.ResourceType = "persistentvolumeclaims", domain.ResourcePersistentVolumeClaim
	default:
		return yamlTarget{}, fmt.Errorf("%w: resource %s/%s is not editable", domain.ErrUnsupported, identity.APIVersion, identity.Kind)
	}
	if namespaced && target.Access.Namespace == "" {
		return yamlTarget{}, fmt.Errorf("%w: metadata.namespace is required", domain.ErrInvalid)
	}
	return target, nil
}

func validateSingleYAMLDocument(body string) error {
	if len(body) > maxYAMLBytes {
		return fmt.Errorf("%w: yaml document must not exceed 1 MiB", domain.ErrInvalid)
	}
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
