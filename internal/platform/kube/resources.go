package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const resourceDiscoveryTTL = 60 * time.Second
const resourceRulesTTL = 30 * time.Second

type cachedResourceRules struct {
	rules     domain.ResourceRules
	warnings  []string
	fetchedAt time.Time
}

// DiscoverResources reads the preferred version of every Kubernetes API group
// and returns resources that can be listed or fetched. The result is cached so
// normal page navigation does not repeatedly walk the API surface.
func (c *Client) DiscoverResources(ctx context.Context) ([]domain.ResourceDescriptor, []string, error) {
	c.resourceDiscoveryMu.Lock()
	defer c.resourceDiscoveryMu.Unlock()

	if len(c.resourceDiscovery) > 0 && time.Since(c.resourceDiscoveryFetched) < resourceDiscoveryTTL {
		return cloneResourceDescriptors(c.resourceDiscovery), append([]string(nil), c.resourceDiscoveryWarnings...), nil
	}

	// API Discovery is a server-level type catalog. Reading it with the
	// application credentials keeps the shared cache independent of the first
	// caller's impersonated identity. Object reads still use the actor and are
	// authorized separately by the application layer.
	ctx, cancel := c.withTimeout(domain.WithoutActor(ctx))
	defer cancel()
	items, warnings, err := c.fetchResourceDiscovery(ctx)
	if err != nil {
		if len(c.resourceDiscovery) > 0 {
			warnings = append(warnings, "API Discovery 刷新失败，当前展示最近一次成功结果")
			return cloneResourceDescriptors(c.resourceDiscovery), warnings, nil
		}
		return nil, warnings, err
	}
	c.resourceDiscovery = cloneResourceDescriptors(items)
	c.resourceDiscoveryWarnings = append([]string(nil), warnings...)
	c.resourceDiscoveryFetched = time.Now()
	return cloneResourceDescriptors(items), append([]string(nil), warnings...), nil
}

func (c *Client) fetchResourceDiscovery(ctx context.Context) ([]domain.ResourceDescriptor, []string, error) {
	var items []domain.ResourceDescriptor
	var warnings []string
	var lastErr error

	core, err := c.serverResourcesForGroupVersion(ctx, "v1")
	if err != nil {
		lastErr = err
		warnings = append(warnings, "无法读取核心 API 资源")
	} else {
		items = append(items, descriptorsFor(core, "")...)
	}

	groups, err := c.serverGroups(ctx)
	if err != nil {
		lastErr = err
		warnings = append(warnings, "无法读取 API 组目录")
	} else {
		for _, group := range groups.Groups {
			// Kubernetes does not expose the core group under /apis, but some
			// discovery implementations (including client-go fakes) include it
			// with an empty group name. The core group is already read above.
			if group.Name == "" || group.PreferredVersion.Version == "" {
				continue
			}
			groupVersion := group.Name + "/" + group.PreferredVersion.Version
			resources, fetchErr := c.serverResourcesForGroupVersion(ctx, groupVersion)
			if fetchErr != nil {
				lastErr = fetchErr
				warnings = append(warnings, fmt.Sprintf("无法读取 API 组 %s", group.Name))
				continue
			}
			items = append(items, descriptorsFor(resources, group.Name)...)
		}
	}

	if len(items) == 0 {
		if lastErr != nil {
			return nil, warnings, fmt.Errorf("%w: %v", domain.ErrUnavailable, lastErr)
		}
		return nil, warnings, fmt.Errorf("%w: Kubernetes API Discovery returned no resources", domain.ErrUnavailable)
	}
	items = uniqueResourceDescriptors(items)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Resource < items[j].Resource
	})
	return items, warnings, nil
}

func descriptorsFor(list *metav1.APIResourceList, group string) []domain.ResourceDescriptor {
	if list == nil {
		return nil
	}
	version := list.GroupVersion
	if group != "" {
		version = strings.TrimPrefix(list.GroupVersion, group+"/")
	}
	out := make([]domain.ResourceDescriptor, 0, len(list.APIResources))
	for _, resource := range list.APIResources {
		if strings.Contains(resource.Name, "/") || resource.Kind == "" {
			continue
		}
		if !hasVerb(resource.Verbs, "list") && !hasVerb(resource.Verbs, "get") {
			continue
		}
		out = append(out, domain.ResourceDescriptor{
			Group:      group,
			Version:    version,
			Resource:   resource.Name,
			Kind:       resource.Kind,
			Singular:   resource.SingularName,
			Namespaced: resource.Namespaced,
			Verbs:      append([]string(nil), resource.Verbs...),
			ShortNames: append([]string(nil), resource.ShortNames...),
			Categories: append([]string(nil), resource.Categories...),
		})
	}
	return out
}

func hasVerb(verbs []string, want string) bool {
	for _, verb := range verbs {
		if verb == want {
			return true
		}
	}
	return false
}

func uniqueResourceDescriptors(items []domain.ResourceDescriptor) []domain.ResourceDescriptor {
	seen := make(map[string]struct{}, len(items))
	out := make([]domain.ResourceDescriptor, 0, len(items))
	for _, item := range items {
		key := item.Group + "/" + item.Version + "/" + item.Resource
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func cloneResourceDescriptors(items []domain.ResourceDescriptor) []domain.ResourceDescriptor {
	out := make([]domain.ResourceDescriptor, len(items))
	for i, item := range items {
		out[i] = item
		out[i].Verbs = append([]string(nil), item.Verbs...)
		out[i].ShortNames = append([]string(nil), item.ShortNames...)
		out[i].Categories = append([]string(nil), item.Categories...)
	}
	return out
}

func (c *Client) ListGenericResources(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if tablePage, err := c.listGenericResourceTable(ctx, descriptor, query, page); err == nil {
		return tablePage, nil
	}
	resource, err := c.genericResourceInterface(descriptor, query.Namespace)
	if err != nil {
		return domain.GenericResourcePage{}, err
	}
	fetch := func(options metav1.ListOptions) ([]unstructured.Unstructured, metav1.ListMeta, error) {
		list, listErr := resource.List(ctx, options)
		if listErr != nil {
			return nil, metav1.ListMeta{}, mapKubeError(listErr)
		}
		return list.Items, metav1.ListMeta{
			Continue:           list.GetContinue(),
			RemainingItemCount: list.GetRemainingItemCount(),
			ResourceVersion:    list.GetResourceVersion(),
		}, nil
	}
	result, err := listPage(ctx, page, query.Search != "", fetch, func(item unstructured.Unstructured) domain.GenericResourceSummary {
		return genericResourceSummary(descriptor, item)
	}, func(item domain.GenericResourceSummary) bool {
		return matchesSearch([]string{item.Name, item.Namespace, item.Kind}, query.Search)
	})
	if err != nil {
		return domain.GenericResourcePage{}, err
	}
	result.Warnings = append(result.Warnings, "该资源类型未返回 Table 列信息，已回退到基础元数据列")
	return domain.GenericResourcePage{
		PageResult: result,
		Columns:    genericFallbackColumns(descriptor),
	}, nil
}

func (c *Client) listGenericResourceTable(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error) {
	if c.discovery == nil || c.discovery.RESTClient() == nil {
		return domain.GenericResourcePage{}, fmt.Errorf("%w: discovery REST client is unavailable", domain.ErrUnavailable)
	}
	restClient := c.discovery.RESTClient()
	path := genericResourcePath(descriptor, query.Namespace)
	var columns []domain.ResourceColumn
	fetch := func(options metav1.ListOptions) ([]metav1.TableRow, metav1.ListMeta, error) {
		request := restClient.Get().AbsPath(path).
			SetHeader("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io").
			Param("limit", strconv.FormatInt(options.Limit, 10))
		if options.Continue != "" {
			request = request.Param("continue", options.Continue)
		}
		body, err := request.DoRaw(ctx)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		var table metav1.Table
		if err := json.Unmarshal(body, &table); err != nil {
			return nil, metav1.ListMeta{}, fmt.Errorf("%w: decode Kubernetes Table response: %v", domain.ErrUnavailable, err)
		}
		if columns == nil {
			columns = tableColumns(table.ColumnDefinitions)
		}
		return table.Rows, metav1.ListMeta{
			Continue:           table.Continue,
			RemainingItemCount: table.RemainingItemCount,
			ResourceVersion:    table.ResourceVersion,
		}, nil
	}
	result, err := listPage(ctx, page, query.Search != "", fetch, func(row metav1.TableRow) domain.GenericResourceSummary {
		summary := tableResourceSummary(descriptor, columns, row)
		if summary.Namespace == "" {
			summary.Namespace = query.Namespace
		}
		return summary
	}, func(item domain.GenericResourceSummary) bool {
		return matchesSearch(append([]string{item.Name, item.Namespace, item.Kind}, resourceCellStrings(item.Cells)...), query.Search)
	})
	if err != nil {
		return domain.GenericResourcePage{}, err
	}
	if len(columns) == 0 {
		columns = genericFallbackColumns(descriptor)
	}
	return domain.GenericResourcePage{PageResult: result, Columns: columns}, nil
}

func genericResourcePath(descriptor domain.ResourceDescriptor, namespace string) string {
	prefix := "/api/" + url.PathEscape(descriptor.Version)
	if descriptor.Group != "" {
		prefix = "/apis/" + url.PathEscape(descriptor.Group) + "/" + url.PathEscape(descriptor.Version)
	}
	if descriptor.Namespaced && namespace != "" {
		return prefix + "/namespaces/" + url.PathEscape(namespace) + "/" + url.PathEscape(descriptor.Resource)
	}
	return prefix + "/" + url.PathEscape(descriptor.Resource)
}

func tableColumns(definitions []metav1.TableColumnDefinition) []domain.ResourceColumn {
	out := make([]domain.ResourceColumn, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "" || definition.Priority > 0 {
			continue
		}
		if _, exists := seen[definition.Name]; exists {
			continue
		}
		seen[definition.Name] = struct{}{}
		out = append(out, domain.ResourceColumn{
			Name:        definition.Name,
			Type:        definition.Type,
			Format:      definition.Format,
			Description: definition.Description,
		})
		if len(out) == 12 {
			break
		}
	}
	return out
}

func genericFallbackColumns(descriptor domain.ResourceDescriptor) []domain.ResourceColumn {
	columns := []domain.ResourceColumn{{Name: "Name", Type: "string"}, {Name: "Kind", Type: "string"}}
	if descriptor.Namespaced {
		columns = append(columns, domain.ResourceColumn{Name: "Namespace", Type: "string"})
	}
	return columns
}

func tableResourceSummary(descriptor domain.ResourceDescriptor, columns []domain.ResourceColumn, row metav1.TableRow) domain.GenericResourceSummary {
	summary := domain.GenericResourceSummary{
		APIVersion: descriptor.Version,
		Kind:       descriptor.Kind,
		Resource:   descriptor.Resource,
		Cells:      make(map[string]any, len(columns)),
	}
	if descriptor.Group != "" {
		summary.APIVersion = descriptor.Group + "/" + descriptor.Version
	}
	for i, column := range columns {
		if i >= len(row.Cells) {
			break
		}
		value := row.Cells[i]
		summary.Cells[column.Name] = value
		switch strings.ToLower(column.Name) {
		case "name":
			if value != nil {
				summary.Name = fmt.Sprint(value)
			}
		case "namespace":
			if value != nil {
				summary.Namespace = fmt.Sprint(value)
			}
		case "uid":
			if value != nil {
				summary.UID = fmt.Sprint(value)
			}
		case "resource version", "resourceversion":
			if value != nil {
				summary.ResourceVersion = fmt.Sprint(value)
			}
		case "created at", "creation timestamp", "creationtimestamp":
			if text, ok := value.(string); ok {
				if parsed, err := time.Parse(time.RFC3339, text); err == nil {
					summary.CreatedAt = parsed
				}
			}
		}
	}
	var object map[string]any
	switch value := row.Object.Object.(type) {
	case *unstructured.Unstructured:
		object = value.Object
	}
	if object == nil && len(row.Object.Raw) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(row.Object.Raw, &decoded); err == nil {
			object = decoded
		}
	}
	if object != nil {
		metadata := objectMapLike(object, "metadata")
		if summary.Name == "" {
			summary.Name = stringMapLike(metadata, "name")
		}
		if summary.Namespace == "" {
			summary.Namespace = stringMapLike(metadata, "namespace")
		}
		summary.UID = stringMapLike(metadata, "uid")
		summary.ResourceVersion = stringMapLike(metadata, "resourceVersion")
		if summary.CreatedAt.IsZero() {
			if parsed, err := time.Parse(time.RFC3339, stringMapLike(metadata, "creationTimestamp")); err == nil {
				summary.CreatedAt = parsed
			}
		}
		summary.Labels = stringMap(metadata, "labels")
		summary.Annotations = stringMap(metadata, "annotations")
	}
	return summary
}

func resourceCellStrings(cells map[string]any) []string {
	values := make([]string, 0, len(cells))
	for _, value := range cells {
		switch typed := value.(type) {
		case string:
			values = append(values, typed)
		case nil:
			continue
		default:
			encoded, err := json.Marshal(typed)
			if err == nil {
				values = append(values, string(encoded))
			} else {
				values = append(values, fmt.Sprint(typed))
			}
		}
	}
	return values
}

func objectMapLike(object map[string]any, key string) map[string]any {
	value, _ := object[key].(map[string]any)
	return value
}

func stringMapLike(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func stringMap(object map[string]any, key string) map[string]string {
	value, _ := object[key].(map[string]any)
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for name, item := range value {
		if text, ok := item.(string); ok {
			out[name] = text
		}
	}
	return out
}

func (c *Client) GetGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (map[string]any, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, namespace)
	if err != nil {
		return nil, err
	}
	item, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	return item.Object, nil
}

func (c *Client) CreateGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, dryRun bool) (map[string]any, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, namespace)
	if err != nil {
		return nil, err
	}
	options := metav1.CreateOptions{FieldManager: "dosh"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	item, err := resource.Create(ctx, &unstructured.Unstructured{Object: object}, options)
	if err != nil {
		return nil, mapKubeError(err)
	}
	return item.Object, nil
}

func (c *Client) UpdateGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, resourceVersion string, dryRun bool) (map[string]any, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, namespace)
	if err != nil {
		return nil, err
	}
	item := &unstructured.Unstructured{Object: object}
	if resourceVersion != "" {
		item.SetResourceVersion(resourceVersion)
	}
	options := metav1.UpdateOptions{FieldManager: "dosh"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	result, err := resource.Update(ctx, item, options)
	if err != nil {
		return nil, mapKubeError(err)
	}
	return result.Object, nil
}

func (c *Client) ApplyGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, resourceVersion string, dryRun bool) (map[string]any, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, namespace)
	if err != nil {
		return nil, err
	}
	item := &unstructured.Unstructured{Object: object}
	if resourceVersion != "" {
		item.SetResourceVersion(resourceVersion)
	}
	body, err := json.Marshal(item.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: encode generic resource: %v", domain.ErrInvalid, err)
	}
	options := metav1.PatchOptions{FieldManager: "dosh"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	result, err := resource.Patch(ctx, item.GetName(), types.ApplyPatchType, body, options)
	if err != nil {
		return nil, mapKubeError(err)
	}
	return result.Object, nil
}

func (c *Client) DeleteGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name, resourceVersion string, propagation domain.DeletionPropagation) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, namespace)
	if err != nil {
		return err
	}
	policy := metav1.DeletePropagationBackground
	switch propagation {
	case domain.DeletePropagationForeground:
		policy = metav1.DeletePropagationForeground
	case domain.DeletePropagationOrphan:
		policy = metav1.DeletePropagationOrphan
	}
	options := metav1.DeleteOptions{PropagationPolicy: &policy}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(resource.Delete(ctx, name, options))
}

func (c *Client) genericResourceInterface(descriptor domain.ResourceDescriptor, namespace string) (dynamic.ResourceInterface, error) {
	if c.dynamic == nil {
		return nil, fmt.Errorf("%w: dynamic Kubernetes client is unavailable", domain.ErrUnavailable)
	}
	gvr := schema.GroupVersionResource{Group: descriptor.Group, Version: descriptor.Version, Resource: descriptor.Resource}
	resource := c.dynamic.Resource(gvr)
	if descriptor.Namespaced && namespace != "" {
		return resource.Namespace(namespace), nil
	}
	return resource, nil
}

func genericResourceSummary(descriptor domain.ResourceDescriptor, item unstructured.Unstructured) domain.GenericResourceSummary {
	return domain.GenericResourceSummary{
		APIVersion:      item.GetAPIVersion(),
		Kind:            item.GetKind(),
		Resource:        descriptor.Resource,
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		UID:             string(item.GetUID()),
		ResourceVersion: item.GetResourceVersion(),
		CreatedAt:       item.GetCreationTimestamp().Time,
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
	}
}

// ListResourceRules returns the normalized namespaced SelfSubjectRulesReview
// result for the authenticated actor. Kubernetes requires a namespace for this
// review, so cluster-scoped resources are authorized by the application layer
// with bounded SubjectAccessReview calls instead.
func (c *Client) ListResourceRules(ctx context.Context, actor domain.Actor, namespace string) (domain.ResourceRules, []string, error) {
	if c.clientset == nil {
		return domain.ResourceRules{}, nil, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	if actor.Username == "" {
		return domain.ResourceRules{}, nil, domain.ErrForbidden
	}
	if namespace == "" {
		return domain.ResourceRules{}, nil, fmt.Errorf("%w: namespace is required for resource rules review", domain.ErrInvalid)
	}
	key := actor.Username + "\x00" + strings.Join(actor.Groups, "\x00") + "\x00" + namespace
	c.resourceRulesMu.Lock()
	if cached, ok := c.resourceRules[key]; ok && time.Since(cached.fetchedAt) < resourceRulesTTL {
		rules := cloneResourceRules(cached.rules)
		warnings := append([]string(nil), cached.warnings...)
		c.resourceRulesMu.Unlock()
		return rules, warnings, nil
	}
	c.resourceRulesMu.Unlock()

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var result domain.ResourceRules
	var warnings []string
	rules, incomplete, err := c.selfSubjectResourceRules(ctx, namespace)
	if err != nil {
		warnings = append(warnings, "无法读取当前命名空间的 RBAC 规则，命名空间资源已从 Discovery 中隐藏")
		return result, warnings, err
	}
	result.Namespaced = rules
	if incomplete {
		warnings = append(warnings, "当前命名空间的 RBAC 规则不完整，部分已授权资源可能被隐藏")
	}

	c.resourceRulesMu.Lock()
	if c.resourceRules == nil {
		c.resourceRules = make(map[string]cachedResourceRules)
	}
	for cachedKey, cached := range c.resourceRules {
		if time.Since(cached.fetchedAt) >= resourceRulesTTL {
			delete(c.resourceRules, cachedKey)
		}
	}
	c.resourceRules[key] = cachedResourceRules{rules: cloneResourceRules(result), warnings: append([]string(nil), warnings...), fetchedAt: time.Now()}
	c.resourceRulesMu.Unlock()
	return result, warnings, nil
}

func (c *Client) selfSubjectResourceRules(ctx context.Context, namespace string) ([]domain.ResourceRule, bool, error) {
	review := &authv1.SelfSubjectRulesReview{Spec: authv1.SelfSubjectRulesReviewSpec{Namespace: namespace}}
	response, err := c.clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, false, mapKubeError(err)
	}
	if response.Status.EvaluationError != "" {
		return nil, false, fmt.Errorf("%w: authorization rules review: %s", domain.ErrUnavailable, response.Status.EvaluationError)
	}
	rules := make([]domain.ResourceRule, 0, len(response.Status.ResourceRules))
	for _, rule := range response.Status.ResourceRules {
		rules = append(rules, domain.ResourceRule{
			Verbs: append([]string(nil), rule.Verbs...), APIGroups: append([]string(nil), rule.APIGroups...),
			Resources: append([]string(nil), rule.Resources...), ResourceNames: append([]string(nil), rule.ResourceNames...),
		})
	}
	return rules, response.Status.Incomplete, nil
}

func cloneResourceRules(rules domain.ResourceRules) domain.ResourceRules {
	return domain.ResourceRules{
		Namespaced: cloneRules(rules.Namespaced),
		Cluster:    cloneRules(rules.Cluster),
	}
}

func cloneRules(rules []domain.ResourceRule) []domain.ResourceRule {
	out := make([]domain.ResourceRule, len(rules))
	for i, rule := range rules {
		out[i] = domain.ResourceRule{
			Verbs: append([]string(nil), rule.Verbs...), APIGroups: append([]string(nil), rule.APIGroups...),
			Resources: append([]string(nil), rule.Resources...), ResourceNames: append([]string(nil), rule.ResourceNames...),
		}
	}
	return out
}
