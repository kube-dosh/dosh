package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const preferencesDataKey = "preferences.json"

func (c *Client) GetUserPreferences(ctx context.Context, key string) (domain.UserPreferences, error) {
	if c.preferencesNamespace == "" {
		return domain.UserPreferences{}, nil
	}
	if c.clientset == nil {
		return domain.UserPreferences{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	// Preferences are stored by the control-plane identity. The key is a
	// one-way hash of the authenticated username, so no user can address
	// another user's object through this API.
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	configMap, err := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace).Get(ctx, preferencesConfigMapName(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.UserPreferences{}, nil
	}
	if err != nil {
		return domain.UserPreferences{}, mapKubeError(err)
	}
	var preferences domain.UserPreferences
	if raw := configMap.Data[preferencesDataKey]; strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &preferences); err != nil {
			return domain.UserPreferences{}, fmt.Errorf("%w: decode user preferences: %v", domain.ErrUnavailable, err)
		}
	}
	return preferences, nil
}

func (c *Client) SaveUserPreferences(ctx context.Context, key string, preferences domain.UserPreferences) (domain.UserPreferences, error) {
	if c.preferencesNamespace == "" {
		return domain.UserPreferences{}, fmt.Errorf("%w: preference storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return domain.UserPreferences{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	encoded, err := json.Marshal(preferences)
	if err != nil {
		return domain.UserPreferences{}, fmt.Errorf("%w: encode user preferences: %v", domain.ErrInvalid, err)
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	name := preferencesConfigMapName(key)
	client := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace)
	for attempt := 0; attempt < 2; attempt++ {
		current, getErr := client.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			_, createErr := client.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: name, Namespace: c.preferencesNamespace,
					Labels:      map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "user-preferences"},
					Annotations: map[string]string{"dosh.io/user-key": key},
				},
				Data: map[string]string{preferencesDataKey: string(encoded)},
			}, metav1.CreateOptions{FieldManager: "dosh"})
			if createErr == nil {
				return preferences, nil
			}
			if apierrors.IsAlreadyExists(createErr) {
				continue
			}
			return domain.UserPreferences{}, mapKubeError(createErr)
		}
		if getErr != nil {
			return domain.UserPreferences{}, mapKubeError(getErr)
		}
		if current.Data == nil {
			current.Data = make(map[string]string)
		}
		current.Data[preferencesDataKey] = string(encoded)
		if current.Labels == nil {
			current.Labels = make(map[string]string)
		}
		current.Labels["app.kubernetes.io/managed-by"] = "dosh"
		current.Labels["dosh.io/component"] = "user-preferences"
		_, updateErr := client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
		if updateErr == nil {
			return preferences, nil
		}
		if apierrors.IsConflict(updateErr) && attempt == 0 {
			continue
		}
		return domain.UserPreferences{}, mapKubeError(updateErr)
	}
	return domain.UserPreferences{}, fmt.Errorf("%w: preference update conflict", domain.ErrConflict)
}

func preferencesConfigMapName(key string) string {
	return "dosh-user-preferences-" + key
}
