package kube

import (
	"context"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func (c *Client) ListEvents(ctx context.Context, query domain.ListQuery) ([]domain.Event, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.Event, 0, len(items.Items))
	for _, item := range items.Items {
		event := convertEvent(item)
		if query.ObjectKind != "" && !strings.EqualFold(event.ObjectKind, query.ObjectKind) {
			continue
		}
		if query.ObjectName != "" && event.ObjectName != query.ObjectName {
			continue
		}
		if query.Search != "" && !matchesSearch([]string{event.Namespace, event.Type, event.Reason, event.Message, event.ObjectKind, event.ObjectName}, query.Search) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	if len(out) > 500 {
		out = out[:500]
	}
	return out, nil
}

func (c *Client) ListEventsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.Event], error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil {
		return domain.PageResult[domain.Event]{}, mapKubeError(err)
	}
	out := make([]domain.Event, 0, len(items.Items))
	for _, item := range items.Items {
		event := convertEvent(item)
		if query.ObjectKind != "" && !strings.EqualFold(event.ObjectKind, query.ObjectKind) {
			continue
		}
		if query.ObjectName != "" && event.ObjectName != query.ObjectName {
			continue
		}
		if query.Search != "" && !matchesSearch([]string{event.Namespace, event.Type, event.Reason, event.Message, event.ObjectKind, event.ObjectName}, query.Search) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	truncated := items.Continue != "" || len(out) > 500
	if len(out) > 500 {
		out = out[:500]
	}
	result := slicePage(out, page)
	result.Truncated = truncated
	if truncated {
		result.Warnings = []string{"事件列表最多保留最近 500 条，请使用命名空间、对象或关键词缩小范围"}
	}
	return result, nil
}

func (c *Client) WatchEvents(ctx context.Context, namespace string) (<-chan domain.Event, <-chan error, error) {
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	watcher, err := c.clientset.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, mapKubeError(err)
	}
	events := make(chan domain.Event)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		defer watcher.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case result, ok := <-watcher.ResultChan():
				if !ok {
					return
				}
				if result.Type == watch.Error {
					errs <- domain.ErrUnavailable
					return
				}
				event, ok := result.Object.(*corev1.Event)
				if !ok || result.Type == watch.Deleted {
					continue
				}
				select {
				case events <- convertEvent(*event):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return events, errs, nil
}

func convertEvent(item corev1.Event) domain.Event {
	first := item.FirstTimestamp.Time
	last := item.LastTimestamp.Time
	if first.IsZero() {
		first = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	if first.IsZero() {
		first = last
	}
	count := item.Count
	if count == 0 {
		count = 1
	}
	return domain.Event{
		UID:        string(item.UID),
		Namespace:  item.Namespace,
		Type:       item.Type,
		Reason:     item.Reason,
		Message:    item.Message,
		ObjectKind: item.InvolvedObject.Kind,
		ObjectName: item.InvolvedObject.Name,
		Count:      count,
		FirstSeen:  first,
		LastSeen:   last,
		Source:     item.Source.Component,
	}
}

func eventTimeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
