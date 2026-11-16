package kube

import (
	"context"
	"fmt"
	"sort"

	"dosh/internal/domain"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	maxResourceRelations = 40
	relationScanLimit    = 200
)

type relationGraph struct {
	items    []domain.ResourceRelation
	seen     map[string]struct{}
	warnings []string
	byGVK    map[string]domain.ResourceDescriptor
}

func newRelationGraph(descriptors []domain.ResourceDescriptor) *relationGraph {
	graph := &relationGraph{items: make([]domain.ResourceRelation, 0), seen: make(map[string]struct{}), byGVK: make(map[string]domain.ResourceDescriptor)}
	for _, descriptor := range descriptors {
		apiVersion := descriptor.Version
		if descriptor.Group != "" {
			apiVersion = descriptor.Group + "/" + descriptor.Version
		}
		key := apiVersion + "/" + descriptor.Kind
		existing, exists := graph.byGVK[key]
		if !exists || (!existing.Preferred && descriptor.Preferred) {
			graph.byGVK[key] = descriptor
		}
	}
	return graph
}

func (g *relationGraph) add(apiVersion, kind, name, namespace, relationship, direction, status string) {
	if name == "" || len(g.items) >= maxResourceRelations {
		return
	}
	resource := ""
	if descriptor, ok := g.byGVK[apiVersion+"/"+kind]; ok {
		resource = descriptor.Resource
	}
	key := apiVersion + "\x00" + kind + "\x00" + namespace + "\x00" + name + "\x00" + relationship
	if _, exists := g.seen[key]; exists {
		return
	}
	g.seen[key] = struct{}{}
	g.items = append(g.items, domain.ResourceRelation{
		APIVersion: apiVersion, Kind: kind, Resource: resource, Name: name, Namespace: namespace,
		Relationship: relationship, Direction: direction, Status: status,
	})
}

func (g *relationGraph) warn(message string) {
	for _, existing := range g.warnings {
		if existing == message {
			return
		}
	}
	g.warnings = append(g.warnings, message)
}

func relationListOptions(labelSelector string) metav1.ListOptions {
	options := metav1.ListOptions{Limit: relationScanLimit}
	if labelSelector != "" {
		options.LabelSelector = labelSelector
	}
	return options
}

func (g *relationGraph) warnTruncated(kind, namespace, continueToken string) {
	if continueToken == "" {
		return
	}
	if namespace == "" {
		namespace = "集群"
	}
	g.warn(fmt.Sprintf("%s %s 的关系扫描达到 %d 条上限，其余对象已省略", namespace, kind, relationScanLimit))
}

// GetResourceRelations returns bounded immediate edges for one object. It favors
// stable Kubernetes ownership and selector semantics over label heuristics. A
// failed related-type list is reported as a warning so a partial RBAC grant can
// still inspect the relationships it is allowed to read.
func (c *Client) GetResourceRelations(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (domain.ResourceRelations, error) {
	object, err := c.GetGenericResource(ctx, descriptor, namespace, name)
	if err != nil {
		return domain.ResourceRelations{}, err
	}
	descriptors, discoveryWarnings, err := c.DiscoverResources(ctx)
	if err != nil {
		return domain.ResourceRelations{}, err
	}
	graph := newRelationGraph(descriptors)
	for _, warning := range discoveryWarnings {
		graph.warn(warning)
	}
	item := &unstructured.Unstructured{Object: object}
	kind := item.GetKind()
	labels := item.GetLabels()
	for _, owner := range item.GetOwnerReferences() {
		graph.add(owner.APIVersion, owner.Kind, owner.Name, namespace, "owner", "upstream", "")
	}
	switch kind {
	case "Pod":
		c.addPodRelations(ctx, graph, item)
	case "Service":
		c.addServiceRelations(ctx, graph, item)
	case "Ingress":
		c.addIngressRelations(ctx, graph, item)
	case "Node":
		c.addNodeRelations(ctx, graph, item)
	case "PersistentVolumeClaim":
		c.addClaimRelations(ctx, graph, item)
	case "PersistentVolume":
		c.addVolumeRelations(ctx, graph, item)
	default:
		c.addWorkloadRelations(ctx, graph, item, labels)
	}
	if len(graph.items) >= maxResourceRelations {
		graph.warn(fmt.Sprintf("关系数量达到 %d 条上限，其余关联对象已省略", maxResourceRelations))
	}
	return domain.ResourceRelations{Items: graph.items, Warnings: graph.warnings}, nil
}

func (c *Client) addPodRelations(ctx context.Context, graph *relationGraph, pod *unstructured.Unstructured) {
	namespace := pod.GetNamespace()
	if nodeName, _, _ := unstructured.NestedString(pod.Object, "spec", "nodeName"); nodeName != "" {
		graph.add("v1", "Node", nodeName, "", "scheduled on", "upstream", "")
	}
	volumes, _, _ := unstructured.NestedSlice(pod.Object, "spec", "volumes")
	for _, raw := range volumes {
		volume, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		claim, _, _ := unstructured.NestedString(volume, "persistentVolumeClaim", "claimName")
		graph.add("v1", "PersistentVolumeClaim", claim, namespace, "mounts", "upstream", "")
	}
	c.addMatchingServices(ctx, graph, namespace, pod.GetLabels())
}

func (c *Client) addServiceRelations(ctx context.Context, graph *relationGraph, service *unstructured.Unstructured) {
	namespace := service.GetNamespace()
	selector, _, _ := unstructured.NestedStringMap(service.Object, "spec", "selector")
	if len(selector) > 0 {
		pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, relationListOptions(selectorString(selector)))
		if err != nil {
			graph.warn(fmt.Sprintf("无法读取 Service %s/%s 关联的 Pod", namespace, service.GetName()))
		} else {
			graph.warnTruncated("Pod", namespace, pods.Continue)
			for _, pod := range pods.Items {
				graph.add("v1", "Pod", pod.Name, pod.Namespace, "selects", "downstream", string(pod.Status.Phase))
				if len(graph.items) >= maxResourceRelations {
					break
				}
			}
		}
	}
	ingresses, err := c.clientset.NetworkingV1().Ingresses(namespace).List(ctx, relationListOptions(""))
	if err != nil {
		graph.warn(fmt.Sprintf("无法读取命名空间 %s 的 Ingress 关系", namespace))
		return
	}
	graph.warnTruncated("Ingress", namespace, ingresses.Continue)
	for _, ingress := range ingresses.Items {
		if ingressReferencesService(ingress, service.GetName()) {
			status := ""
			if len(ingress.Status.LoadBalancer.Ingress) > 0 {
				status = ingress.Status.LoadBalancer.Ingress[0].IP
			}
			graph.add("networking.k8s.io/v1", "Ingress", ingress.Name, ingress.Namespace, "routes traffic", "upstream", status)
		}
	}
}

func (c *Client) addIngressRelations(ctx context.Context, graph *relationGraph, ingress *unstructured.Unstructured) {
	namespace := ingress.GetNamespace()
	services := ingressServiceNames(ingress.Object)
	for _, service := range services {
		graph.add("v1", "Service", service, namespace, "routes to", "downstream", "")
	}
}

func (c *Client) addNodeRelations(ctx context.Context, graph *relationGraph, node *unstructured.Unstructured) {
	options := relationListOptions("")
	options.FieldSelector = "spec.nodeName=" + node.GetName()
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, options)
	if err != nil {
		graph.warn("无法读取节点上的 Pod，可能缺少跨命名空间 list pods 权限")
		return
	}
	graph.warnTruncated("Pod", "", pods.Continue)
	for _, pod := range pods.Items {
		graph.add("v1", "Pod", pod.Name, pod.Namespace, "runs on", "downstream", string(pod.Status.Phase))
		if len(graph.items) >= maxResourceRelations {
			return
		}
	}
}

func (c *Client) addClaimRelations(ctx context.Context, graph *relationGraph, claim *unstructured.Unstructured) {
	namespace := claim.GetNamespace()
	if volumeName, _, _ := unstructured.NestedString(claim.Object, "spec", "volumeName"); volumeName != "" {
		graph.add("v1", "PersistentVolume", volumeName, "", "bound to", "upstream", "")
	}
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, relationListOptions(""))
	if err != nil {
		graph.warn(fmt.Sprintf("无法读取 PVC %s/%s 关联的 Pod", namespace, claim.GetName()))
		return
	}
	graph.warnTruncated("Pod", namespace, pods.Continue)
	for _, pod := range pods.Items {
		if podUsesClaim(pod, claim.GetName()) {
			graph.add("v1", "Pod", pod.Name, pod.Namespace, "mounts", "downstream", string(pod.Status.Phase))
		}
	}
}

func (c *Client) addVolumeRelations(ctx context.Context, graph *relationGraph, volume *unstructured.Unstructured) {
	claimNamespace, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "namespace")
	claimName, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "name")
	graph.add("v1", "PersistentVolumeClaim", claimName, claimNamespace, "bound to", "downstream", "")
}

func (c *Client) addWorkloadRelations(ctx context.Context, graph *relationGraph, workload *unstructured.Unstructured, labels map[string]string) {
	namespace := workload.GetNamespace()
	selector := workloadSelector(workload.Object)
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, relationListOptions(selectorString(selector)))
	if err != nil {
		graph.warn(fmt.Sprintf("无法读取 %s 关联的 Pod", workload.GetKind()))
	} else {
		graph.warnTruncated("Pod", namespace, pods.Continue)
		podLabels := make(map[string]string)
		for key, value := range labels {
			podLabels[key] = value
		}
		if templateLabels, _, _ := unstructured.NestedStringMap(workload.Object, "spec", "template", "metadata", "labels"); len(templateLabels) > 0 {
			for key, value := range templateLabels {
				podLabels[key] = value
			}
		}
		for _, pod := range pods.Items {
			if !podOwnedBy(pod, workload) && !mapContainsAll(pod.Labels, selector) {
				continue
			}
			graph.add("v1", "Pod", pod.Name, pod.Namespace, "manages", "downstream", string(pod.Status.Phase))
			for key, value := range pod.Labels {
				podLabels[key] = value
			}
			if len(graph.items) >= maxResourceRelations {
				return
			}
		}
		c.addMatchingServices(ctx, graph, namespace, podLabels)
	}
	if workload.GetKind() == "CronJob" {
		jobs, err := c.clientset.BatchV1().Jobs(namespace).List(ctx, relationListOptions(""))
		if err != nil {
			graph.warn(fmt.Sprintf("无法读取 CronJob %s/%s 关联的 Job", namespace, workload.GetName()))
			return
		}
		graph.warnTruncated("Job", namespace, jobs.Continue)
		for _, job := range jobs.Items {
			if jobOwnedBy(job.OwnerReferences, workload) {
				graph.add("batch/v1", "Job", job.Name, job.Namespace, "creates", "downstream", jobStatus(job))
			}
		}
	}
}

func (c *Client) addMatchingServices(ctx context.Context, graph *relationGraph, namespace string, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	services, err := c.clientset.CoreV1().Services(namespace).List(ctx, relationListOptions(""))
	if err != nil {
		graph.warn(fmt.Sprintf("无法读取命名空间 %s 的 Service 关系", namespace))
		return
	}
	graph.warnTruncated("Service", namespace, services.Continue)
	for _, service := range services.Items {
		if len(service.Spec.Selector) > 0 && mapContainsAll(labels, service.Spec.Selector) {
			graph.add("v1", "Service", service.Name, service.Namespace, "exposes", "related", string(service.Spec.Type))
		}
	}
}

func workloadSelector(object map[string]any) map[string]string {
	if selector, _, _ := unstructured.NestedStringMap(object, "spec", "selector", "matchLabels"); len(selector) > 0 {
		return selector
	}
	if selector, _, _ := unstructured.NestedStringMap(object, "spec", "selector"); len(selector) > 0 {
		return selector
	}
	if selector, _, _ := unstructured.NestedStringMap(object, "spec", "jobTemplate", "spec", "selector", "matchLabels"); len(selector) > 0 {
		return selector
	}
	return nil
}

func podOwnedBy(pod corev1.Pod, workload *unstructured.Unstructured) bool {
	return jobOwnedBy(pod.OwnerReferences, workload)
}

func jobOwnedBy(owners []metav1.OwnerReference, workload *unstructured.Unstructured) bool {
	for _, owner := range owners {
		if owner.UID != "" && owner.UID == workload.GetUID() {
			return true
		}
		if owner.Kind == workload.GetKind() && owner.Name == workload.GetName() {
			return true
		}
	}
	return false
}

func mapContainsAll(values, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, expected := range selector {
		if values[key] != expected {
			return false
		}
	}
	return true
}

func podUsesClaim(pod corev1.Pod, claim string) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == claim {
			return true
		}
	}
	return false
}

func ingressReferencesService(ingress networkingv1.Ingress, service string) bool {
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil && ingress.Spec.DefaultBackend.Service.Name == service {
		return true
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil && path.Backend.Service.Name == service {
				return true
			}
		}
	}
	return false
}

func ingressServiceNames(object map[string]any) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	add := func(value string) {
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if value, _, _ := unstructured.NestedString(object, "spec", "defaultBackend", "service", "name"); value != "" {
		add(value)
	}
	rules, _, _ := unstructured.NestedSlice(object, "spec", "rules")
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		paths, _, _ := unstructured.NestedSlice(rule, "http", "paths")
		for _, rawPath := range paths {
			path, ok := rawPath.(map[string]any)
			if !ok {
				continue
			}
			if value, _, _ := unstructured.NestedString(path, "backend", "service", "name"); value != "" {
				add(value)
			}
		}
	}
	sort.Strings(result)
	return result
}

func jobStatus(job batchv1.Job) string {
	if job.Status.Active > 0 {
		return "Active"
	}
	if job.Status.Succeeded > 0 {
		return "Succeeded"
	}
	if job.Status.Failed > 0 {
		return "Failed"
	}
	return ""
}
