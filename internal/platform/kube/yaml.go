package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dosh/internal/domain"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

func (c *Client) Exists(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (bool, error) {
	gvr, namespaced, ok := gvrFor(resourceType)
	if !ok {
		return false, fmt.Errorf("%w: unsupported resource type %q", domain.ErrUnsupported, resourceType)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource := c.dynamic.Resource(gvr)
	var err error
	if namespaced {
		if namespace == "" {
			return false, fmt.Errorf("%w: namespace is required", domain.ErrInvalid)
		}
		_, err = resource.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		_, err = resource.Get(ctx, name, metav1.GetOptions{})
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, mapKubeError(err)
	}
	return true, nil
}

func (c *Client) GetYAML(ctx context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error) {
	gvr, namespaced, ok := gvrFor(resourceType)
	if !ok {
		return domain.YAMLResource{}, fmt.Errorf("%w: unsupported resource type %q", domain.ErrUnsupported, resourceType)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var resource = c.dynamic.Resource(gvr)
	var item *unstructured.Unstructured
	var err error
	if namespaced {
		if namespace == "" {
			return domain.YAMLResource{}, fmt.Errorf("%w: namespace is required", domain.ErrInvalid)
		}
		item, err = resource.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		item, err = resource.Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return domain.YAMLResource{}, mapKubeError(err)
	}
	obj := item.DeepCopy()
	obj.SetManagedFields(nil)
	if resourceType == domain.ResourceSecret {
		redactSecret(obj)
	}
	data, err := yaml.Marshal(obj.Object)
	if err != nil {
		return domain.YAMLResource{}, fmt.Errorf("marshal yaml: %w", err)
	}
	return domain.YAMLResource{Kind: resourceTypeString(resourceType), Namespace: namespace, Name: name, YAML: string(data)}, nil
}

func (c *Client) ApplyYAML(ctx context.Context, request domain.ApplyRequest) (domain.YAMLResource, error) {
	if strings.TrimSpace(request.YAML) == "" {
		return domain.YAMLResource{}, fmt.Errorf("%w: yaml body is required", domain.ErrInvalid)
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(request.YAML), &raw); err != nil {
		return domain.YAMLResource{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	obj := &unstructured.Unstructured{Object: raw}
	apiVersion := obj.GetAPIVersion()
	kind := obj.GetKind()
	resourceType, namespaced, ok := resourceTypeFromGVK(apiVersion, kind)
	if !ok {
		return domain.YAMLResource{}, fmt.Errorf("%w: resource %s/%s is not editable", domain.ErrUnsupported, apiVersion, kind)
	}
	name := obj.GetName()
	if name == "" {
		return domain.YAMLResource{}, fmt.Errorf("%w: metadata.name is required", domain.ErrInvalid)
	}
	namespace := obj.GetNamespace()
	if namespaced && namespace == "" {
		return domain.YAMLResource{}, fmt.Errorf("%w: metadata.namespace is required", domain.ErrInvalid)
	}
	if request.ResourceVersion != "" {
		obj.SetResourceVersion(request.ResourceVersion)
	}
	gvr, _, _ := gvrFor(resourceType)
	body, err := json.Marshal(obj.Object)
	if err != nil {
		return domain.YAMLResource{}, fmt.Errorf("encode yaml object: %w", err)
	}
	options := metav1.PatchOptions{FieldManager: "dosh"}
	if request.DryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var result *unstructured.Unstructured
	if namespaced {
		result, err = c.dynamic.Resource(gvr).Namespace(namespace).Patch(ctx, name, types.ApplyPatchType, body, options)
	} else {
		result, err = c.dynamic.Resource(gvr).Patch(ctx, name, types.ApplyPatchType, body, options)
	}
	if err != nil {
		return domain.YAMLResource{}, mapKubeError(err)
	}
	result.SetManagedFields(nil)
	if resourceType == domain.ResourceSecret {
		redactSecret(result)
	}
	data, err := yaml.Marshal(result.Object)
	if err != nil {
		return domain.YAMLResource{}, fmt.Errorf("marshal applied yaml: %w", err)
	}
	return domain.YAMLResource{Kind: resourceTypeString(resourceType), Namespace: namespace, Name: name, YAML: string(data)}, nil
}

func redactSecret(obj *unstructured.Unstructured) {
	if data, ok := obj.Object["data"].(map[string]any); ok {
		for key := range data {
			data[key] = "<redacted>"
		}
	}
	if data, ok := obj.Object["stringData"].(map[string]any); ok {
		for key := range data {
			data[key] = "<redacted>"
		}
	}
}

func resourceTypeFromGVK(apiVersion, kind string) (domain.ResourceType, bool, bool) {
	switch apiVersion + "/" + kind {
	case "v1/Pod":
		return domain.ResourcePod, true, true
	case "apps/v1/Deployment":
		return domain.ResourceDeployment, true, true
	case "apps/v1/StatefulSet":
		return domain.ResourceStatefulSet, true, true
	case "apps/v1/DaemonSet":
		return domain.ResourceDaemonSet, true, true
	case "batch/v1/Job":
		return domain.ResourceJob, true, true
	case "batch/v1/CronJob":
		return domain.ResourceCronJob, true, true
	case "v1/Service":
		return domain.ResourceService, true, true
	case "networking.k8s.io/v1/Ingress":
		return domain.ResourceIngress, true, true
	case "v1/ConfigMap":
		return domain.ResourceConfigMap, true, true
	case "v1/Secret":
		return domain.ResourceSecret, true, true
	case "storage.k8s.io/v1/StorageClass":
		return domain.ResourceStorageClass, false, true
	case "v1/PersistentVolume":
		return domain.ResourcePersistentVolume, false, true
	case "v1/PersistentVolumeClaim":
		return domain.ResourcePersistentVolumeClaim, true, true
	default:
		return "", false, false
	}
}

func resourceTypeString(resourceType domain.ResourceType) string {
	switch resourceType {
	case domain.ResourceNode:
		return "Node"
	case domain.ResourceNamespace:
		return "Namespace"
	case domain.ResourcePod:
		return "Pod"
	case domain.ResourceDeployment:
		return "Deployment"
	case domain.ResourceStatefulSet:
		return "StatefulSet"
	case domain.ResourceDaemonSet:
		return "DaemonSet"
	case domain.ResourceJob:
		return "Job"
	case domain.ResourceCronJob:
		return "CronJob"
	case domain.ResourceService:
		return "Service"
	case domain.ResourceIngress:
		return "Ingress"
	case domain.ResourceConfigMap:
		return "ConfigMap"
	case domain.ResourceSecret:
		return "Secret"
	case domain.ResourceStorageClass:
		return "StorageClass"
	case domain.ResourcePersistentVolume:
		return "PersistentVolume"
	case domain.ResourcePersistentVolumeClaim:
		return "PersistentVolumeClaim"
	default:
		return string(resourceType)
	}
}
