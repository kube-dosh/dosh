package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const gatewayAPIGroup = "gateway.networking.k8s.io"

var gatewayAPIResources = []string{"gatewayclasses", "gateways", "httproutes", "grpcroutes"}

// GatewayAPIOverview discovers Gateway API CRDs at runtime and presents the
// commonly operated objects without requiring Dosh to ship separate clients for
// every Gateway API release channel.
func (c *Client) GatewayAPIOverview(ctx context.Context, namespace string) (domain.GatewayAPIOverview, error) {
	overview := domain.GatewayAPIOverview{
		Classes: []domain.GatewayClassSummary{}, Gateways: []domain.GatewaySummary{}, Routes: []domain.GatewayRouteSummary{},
		GeneratedAt: time.Now().UTC(),
	}
	if c.dynamic == nil {
		overview.Warnings = append(overview.Warnings, "动态 Kubernetes 客户端不可用，无法读取 Gateway API")
		return overview, nil
	}
	descriptors, discoveryWarnings, err := c.DiscoverResources(domain.WithoutActor(ctx))
	overview.Warnings = append(overview.Warnings, discoveryWarnings...)
	if err != nil {
		overview.Warnings = append(overview.Warnings, "无法执行 API Discovery："+err.Error())
		return overview, nil
	}
	resources := preferredGatewayAPIResources(descriptors)
	if len(resources) == 0 {
		overview.Warnings = append(overview.Warnings, "当前集群未安装 Gateway API CRD")
		return overview, nil
	}
	overview.Available = true
	for _, resource := range gatewayAPIResources {
		descriptor, ok := resources[resource]
		if !ok {
			continue
		}
		if overview.Version == "" {
			overview.Version = descriptor.Version
		}
		gvr := schema.GroupVersionResource{Group: descriptor.Group, Version: descriptor.Version, Resource: resource}
		items, warnings := c.listGatewayAPIObjects(ctx, gvr, resource == "gatewayclasses", namespace)
		overview.Warnings = append(overview.Warnings, warnings...)
		if resource == "gatewayclasses" {
			overview.Classes = append(overview.Classes, gatewayClassSummaries(items, descriptor)...)
		}
		if resource == "gateways" {
			overview.Gateways = append(overview.Gateways, gatewaySummaries(items, descriptor)...)
		}
		if resource == "httproutes" || resource == "grpcroutes" {
			overview.Routes = append(overview.Routes, gatewayRouteSummaries(items, descriptor)...)
		}
	}
	sort.Slice(overview.Classes, func(i, j int) bool { return overview.Classes[i].Name < overview.Classes[j].Name })
	sort.Slice(overview.Gateways, func(i, j int) bool {
		if overview.Gateways[i].Namespace == overview.Gateways[j].Namespace {
			return overview.Gateways[i].Name < overview.Gateways[j].Name
		}
		return overview.Gateways[i].Namespace < overview.Gateways[j].Namespace
	})
	sort.Slice(overview.Routes, func(i, j int) bool {
		if overview.Routes[i].Namespace == overview.Routes[j].Namespace {
			if overview.Routes[i].Kind == overview.Routes[j].Kind {
				return overview.Routes[i].Name < overview.Routes[j].Name
			}
			return overview.Routes[i].Kind < overview.Routes[j].Kind
		}
		return overview.Routes[i].Namespace < overview.Routes[j].Namespace
	})
	return overview, nil
}

func preferredGatewayAPIResources(descriptors []domain.ResourceDescriptor) map[string]domain.ResourceDescriptor {
	out := map[string]domain.ResourceDescriptor{}
	for _, descriptor := range descriptors {
		if descriptor.Group != gatewayAPIGroup {
			continue
		}
		if !containsString(gatewayAPIResources, descriptor.Resource) {
			continue
		}
		current, ok := out[descriptor.Resource]
		if !ok || (!current.Preferred && descriptor.Preferred) || (current.Preferred == descriptor.Preferred && descriptor.Version > current.Version) {
			out[descriptor.Resource] = descriptor
		}
	}
	return out
}

func (c *Client) listGatewayAPIObjects(ctx context.Context, gvr schema.GroupVersionResource, clusterScoped bool, namespace string) ([]unstructured.Unstructured, []string) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource := c.dynamic.Resource(gvr)
	var list *unstructured.UnstructuredList
	var err error
	if clusterScoped {
		list, err = resource.List(ctx, metav1.ListOptions{Limit: 200})
	} else if namespace != "" {
		list, err = resource.Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 200})
	} else {
		list, err = resource.Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{Limit: 200})
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("无法读取 %s：%s", gvr.Resource, err.Error())}
	}
	warnings := []string{}
	if list.GetContinue() != "" {
		warnings = append(warnings, fmt.Sprintf("%s 超过 200 条，当前仅展示首批结果", gvr.Resource))
	}
	return list.Items, warnings
}

func gatewayClassSummaries(items []unstructured.Unstructured, descriptor domain.ResourceDescriptor) []domain.GatewayClassSummary {
	out := make([]domain.GatewayClassSummary, 0, len(items))
	for _, item := range items {
		conditions := gatewayConditions(item)
		controller, _, _ := unstructured.NestedString(item.Object, "spec", "controllerName")
		out = append(out, domain.GatewayClassSummary{
			Name: item.GetName(), Controller: controller, Status: gatewayStatus(conditions, []string{"Accepted"}, false),
			Conditions: conditions, CreatedAt: item.GetCreationTimestamp().Time, ResourceVersion: item.GetResourceVersion(),
			APIVersion: descriptor.Group + "/" + descriptor.Version, Resource: descriptor.Resource,
		})
	}
	return out
}

func gatewaySummaries(items []unstructured.Unstructured, descriptor domain.ResourceDescriptor) []domain.GatewaySummary {
	out := make([]domain.GatewaySummary, 0, len(items))
	for _, item := range items {
		className, _, _ := unstructured.NestedString(item.Object, "spec", "gatewayClassName")
		conditions := gatewayConditions(item)
		summary := domain.GatewaySummary{
			Name: item.GetName(), Namespace: item.GetNamespace(), ClassName: className,
			Addresses: gatewayAddresses(item), Listeners: gatewayListeners(item), Conditions: conditions,
			Status:    gatewayStatus(conditions, []string{"Accepted", "Programmed"}, true),
			CreatedAt: item.GetCreationTimestamp().Time, ResourceVersion: item.GetResourceVersion(),
			APIVersion: descriptor.Group + "/" + descriptor.Version, Resource: descriptor.Resource,
		}
		out = append(out, summary)
	}
	return out
}

func gatewayRouteSummaries(items []unstructured.Unstructured, descriptor domain.ResourceDescriptor) []domain.GatewayRouteSummary {
	out := make([]domain.GatewayRouteSummary, 0, len(items))
	for _, item := range items {
		conditions := gatewayRouteConditions(item)
		summary := domain.GatewayRouteSummary{
			Kind: item.GetKind(), Name: item.GetName(), Namespace: item.GetNamespace(), Hostnames: stringSlice(item.Object, "spec", "hostnames"),
			ParentRefs: gatewayParentRefs(item.GetNamespace(), item.Object), Rules: gatewayRouteRules(item.GetNamespace(), item.Object),
			Conditions: conditions, Status: gatewayStatus(conditions, []string{"Accepted", "ResolvedRefs"}, true),
			CreatedAt: item.GetCreationTimestamp().Time, ResourceVersion: item.GetResourceVersion(),
			APIVersion: descriptor.Group + "/" + descriptor.Version, Resource: descriptor.Resource,
		}
		if summary.Kind == "" {
			summary.Kind = descriptor.Kind
		}
		out = append(out, summary)
	}
	return out
}

func gatewayConditions(item unstructured.Unstructured) []domain.Condition {
	values, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	return conditionsFromSlice(values)
}

func gatewayRouteConditions(item unstructured.Unstructured) []domain.Condition {
	statuses, _, _ := unstructured.NestedSlice(item.Object, "status", "parents")
	var out []domain.Condition
	for _, value := range statuses {
		parent, ok := value.(map[string]any)
		if !ok {
			continue
		}
		conditions, _, _ := unstructured.NestedSlice(parent, "conditions")
		out = append(out, conditionsFromSlice(conditions)...)
	}
	if len(out) == 0 {
		return gatewayConditions(item)
	}
	return out
}

func conditionsFromSlice(values []any) []domain.Condition {
	out := make([]domain.Condition, 0, len(values))
	for _, value := range values {
		condition, ok := value.(map[string]any)
		if !ok {
			continue
		}
		item := domain.Condition{
			Type: stringValue(condition["type"]), Status: stringValue(condition["status"]),
			Reason: stringValue(condition["reason"]), Message: stringValue(condition["message"]),
		}
		if timestamp, ok := condition["lastTransitionTime"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, timestamp); err == nil {
				item.LastTransitionTime = parsed
			}
		}
		out = append(out, item)
	}
	return out
}

func gatewayStatus(conditions []domain.Condition, required []string, requireAll bool) string {
	if len(conditions) == 0 {
		return "Unknown"
	}
	found := 0
	for _, condition := range conditions {
		if condition.Status == "False" {
			for _, wanted := range required {
				if condition.Type == wanted {
					return "Degraded"
				}
			}
		}
	}
	for _, wanted := range required {
		for _, condition := range conditions {
			if condition.Type == wanted && condition.Status == "True" {
				found++
				break
			}
		}
	}
	if requireAll && found == len(required) {
		return "Ready"
	}
	if !requireAll && found > 0 {
		return "Ready"
	}
	for _, condition := range conditions {
		if condition.Status == "True" {
			return "Ready"
		}
	}
	return "Unknown"
}

func gatewayAddresses(item unstructured.Unstructured) []domain.GatewayAPIAddress {
	values, _, _ := unstructured.NestedSlice(item.Object, "status", "addresses")
	out := make([]domain.GatewayAPIAddress, 0, len(values))
	for _, value := range values {
		address, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, domain.GatewayAPIAddress{Type: stringValue(address["type"]), Value: stringValue(address["value"])})
	}
	return out
}

func gatewayListeners(item unstructured.Unstructured) []domain.GatewayAPIListener {
	values, _, _ := unstructured.NestedSlice(item.Object, "spec", "listeners")
	out := make([]domain.GatewayAPIListener, 0, len(values))
	for _, value := range values {
		listener, ok := value.(map[string]any)
		if !ok {
			continue
		}
		port, _ := int64Value(listener["port"])
		item := domain.GatewayAPIListener{
			Name: stringValue(listener["name"]), Protocol: stringValue(listener["protocol"]), Port: int32(port),
			Hostname: stringValue(listener["hostname"]),
		}
		if tls, ok := listener["tls"].(map[string]any); ok {
			item.TLSMode = stringValue(tls["mode"])
		}
		if allowedRoutes, ok := listener["allowedRoutes"].(map[string]any); ok {
			if namespaces, ok := allowedRoutes["namespaces"].(map[string]any); ok {
				item.AllowedRoutes = stringValue(namespaces["from"])
			}
		}
		conditions, _, _ := unstructured.NestedSlice(listener, "status", "conditions")
		item.Conditions = conditionsFromSlice(conditions)
		out = append(out, item)
	}
	return out
}

func gatewayParentRefs(routeNamespace string, object map[string]any) []domain.GatewayParentReference {
	values, _, _ := unstructured.NestedSlice(object, "spec", "parentRefs")
	out := make([]domain.GatewayParentReference, 0, len(values))
	for _, value := range values {
		ref, ok := value.(map[string]any)
		if !ok {
			continue
		}
		port, _ := int64Value(ref["port"])
		item := domain.GatewayParentReference{
			Group: stringValue(ref["group"]), Kind: stringValue(ref["kind"]), Namespace: stringValue(ref["namespace"]),
			Name: stringValue(ref["name"]), SectionName: stringValue(ref["sectionName"]), Port: int32(port),
		}
		if item.Group == "" {
			item.Group = gatewayAPIGroup
		}
		if item.Kind == "" {
			item.Kind = "Gateway"
		}
		if item.Namespace == "" {
			item.Namespace = routeNamespace
		}
		out = append(out, item)
	}
	return out
}

func gatewayRouteRules(routeNamespace string, object map[string]any) []domain.GatewayRouteRule {
	values, _, _ := unstructured.NestedSlice(object, "spec", "rules")
	out := make([]domain.GatewayRouteRule, 0, len(values))
	for _, value := range values {
		rule, ok := value.(map[string]any)
		if !ok {
			continue
		}
		item := domain.GatewayRouteRule{}
		matches, _, _ := unstructured.NestedSlice(rule, "matches")
		for _, matchValue := range matches {
			match, ok := matchValue.(map[string]any)
			if !ok {
				continue
			}
			routeMatch := domain.GatewayRouteMatch{Method: stringValue(match["method"])}
			if path, ok := match["path"].(map[string]any); ok {
				routeMatch.Path = stringValue(path["value"])
				routeMatch.PathType = stringValue(path["type"])
			}
			if headers, _, _ := unstructured.NestedSlice(match, "headers"); len(headers) > 0 {
				for _, headerValue := range headers {
					if header, ok := headerValue.(map[string]any); ok {
						routeMatch.Headers = append(routeMatch.Headers, stringValue(header["name"])+"="+stringValue(header["value"]))
					}
				}
			}
			if query, _, _ := unstructured.NestedSlice(match, "queryParams"); len(query) > 0 {
				for _, queryValue := range query {
					if parameter, ok := queryValue.(map[string]any); ok {
						routeMatch.Query = append(routeMatch.Query, stringValue(parameter["name"])+"="+stringValue(parameter["value"]))
					}
				}
			}
			if method, ok := match["method"].(map[string]any); ok {
				routeMatch.Method = stringValue(method["service"]) + "/" + stringValue(method["method"])
			}
			item.Matches = append(item.Matches, routeMatch)
		}
		filters, _, _ := unstructured.NestedSlice(rule, "filters")
		for _, filterValue := range filters {
			if filter, ok := filterValue.(map[string]any); ok {
				item.Filters = append(item.Filters, stringValue(filter["type"]))
			}
		}
		backends, _, _ := unstructured.NestedSlice(rule, "backendRefs")
		for _, backendValue := range backends {
			backend, ok := backendValue.(map[string]any)
			if !ok {
				continue
			}
			weight, _ := int64Value(backend["weight"])
			item.Backends = append(item.Backends, domain.GatewayRouteBackend{
				Group: stringValue(backend["group"]), Kind: stringValue(backend["kind"]), Namespace: stringValue(backend["namespace"]),
				Name: stringValue(backend["name"]), Port: stringValue(backend["port"]), Weight: int32(weight),
			})
		}
		out = append(out, item)
	}
	return out
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringSlice(object map[string]any, fields ...string) []string {
	values, _, _ := unstructured.NestedStringSlice(object, fields...)
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
