package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const resourceDiscoveryTTL = 60 * time.Second

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

func (c *Client) ListGenericResources(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.GenericResourceSummary], error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource, err := c.genericResourceInterface(descriptor, query.Namespace)
	if err != nil {
		return domain.PageResult[domain.GenericResourceSummary]{}, err
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
	return listPage(ctx, page, query.Search != "", fetch, genericResourceSummary, func(item domain.GenericResourceSummary) bool {
		return matchesSearch([]string{item.Name, item.Namespace, item.Kind}, query.Search)
	})
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

func genericResourceSummary(item unstructured.Unstructured) domain.GenericResourceSummary {
	return domain.GenericResourceSummary{
		APIVersion:      item.GetAPIVersion(),
		Kind:            item.GetKind(),
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		UID:             string(item.GetUID()),
		ResourceVersion: item.GetResourceVersion(),
		CreatedAt:       item.GetCreationTimestamp().Time,
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
	}
}
