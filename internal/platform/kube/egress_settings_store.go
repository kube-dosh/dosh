package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	egressSettingsSecretName = "dosh-egress-settings"
	egressSettingsDataKey    = "settings.json"
	egressSettingsMaxBytes   = 64 << 10
)

var egressSettingsStoreMu sync.Mutex

func (c *Client) LoadEgressSettings(ctx context.Context) (egress.StoredSettings, bool, error) {
	if c.preferencesNamespace == "" {
		return egress.StoredSettings{}, false, fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return egress.StoredSettings{}, false, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	secret, err := c.clientset.CoreV1().Secrets(c.preferencesNamespace).Get(ctx, egressSettingsSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return egress.StoredSettings{}, false, nil
	}
	if err != nil {
		return egress.StoredSettings{}, false, mapKubeError(err)
	}
	data := secret.Data[egressSettingsDataKey]
	if len(data) == 0 {
		return egress.StoredSettings{}, false, nil
	}
	var settings egress.StoredSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return egress.StoredSettings{}, false, fmt.Errorf("%w: decode egress settings: %v", domain.ErrUnavailable, err)
	}
	settings.Revision = secret.ResourceVersion
	return settings, true, nil
}

func (c *Client) SaveEgressSettings(ctx context.Context, settings egress.StoredSettings, expectedRevision string) (egress.StoredSettings, error) {
	if c.preferencesNamespace == "" {
		return egress.StoredSettings{}, fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return egress.StoredSettings{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	egressSettingsStoreMu.Lock()
	defer egressSettingsStoreMu.Unlock()
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	settings.UpdatedAt = time.Now().UTC()
	encoded, err := json.Marshal(settings)
	if err != nil {
		return egress.StoredSettings{}, fmt.Errorf("%w: encode egress settings: %v", domain.ErrInvalid, err)
	}
	if len(encoded) > egressSettingsMaxBytes {
		return egress.StoredSettings{}, fmt.Errorf("%w: egress settings exceed 64 KiB", domain.ErrInvalid)
	}

	client := c.clientset.CoreV1().Secrets(c.preferencesNamespace)
	current, err := client.Get(ctx, egressSettingsSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if expectedRevision != "" {
			return egress.StoredSettings{}, fmt.Errorf("%w: egress settings no longer exist", domain.ErrConflict)
		}
		created, createErr := client.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: egressSettingsSecretName, Namespace: c.preferencesNamespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "egress-settings"},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{egressSettingsDataKey: encoded},
		}, metav1.CreateOptions{FieldManager: "dosh"})
		if createErr != nil {
			if apierrors.IsAlreadyExists(createErr) {
				return egress.StoredSettings{}, fmt.Errorf("%w: egress settings changed; reload and try again", domain.ErrConflict)
			}
			return egress.StoredSettings{}, mapKubeError(createErr)
		}
		settings.Revision = created.ResourceVersion
		return settings, nil
	}
	if err != nil {
		return egress.StoredSettings{}, mapKubeError(err)
	}
	if expectedRevision != "" && expectedRevision != current.ResourceVersion {
		return egress.StoredSettings{}, fmt.Errorf("%w: egress settings changed; reload and try again", domain.ErrConflict)
	}
	if current.Data == nil {
		current.Data = map[string][]byte{}
	}
	current.Data[egressSettingsDataKey] = encoded
	updated, err := client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
	if err != nil {
		if apierrors.IsConflict(err) {
			return egress.StoredSettings{}, fmt.Errorf("%w: egress settings changed; reload and try again", domain.ErrConflict)
		}
		return egress.StoredSettings{}, mapKubeError(err)
	}
	settings.Revision = updated.ResourceVersion
	return settings, nil
}
