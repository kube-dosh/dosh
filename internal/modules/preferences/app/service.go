package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"dosh/internal/domain"
)

const maxPreferencesBytes = 128 << 10

type Repository interface {
	GetUserPreferences(ctx context.Context, key string) (domain.UserPreferences, error)
	SaveUserPreferences(ctx context.Context, key string, preferences domain.UserPreferences) (domain.UserPreferences, error)
}

type Service struct {
	repository Repository
	auditor    domain.Auditor
}

func New(repository Repository, auditor domain.Auditor) *Service {
	return &Service{repository: repository, auditor: auditor}
}

func (s *Service) Get(ctx context.Context, actor domain.Actor) (domain.UserPreferences, error) {
	key, err := userKey(actor)
	if err != nil {
		return domain.UserPreferences{}, err
	}
	preferences, err := s.repository.GetUserPreferences(ctx, key)
	if err != nil {
		return domain.UserPreferences{}, err
	}
	return normalize(preferences), nil
}

func (s *Service) Save(ctx context.Context, actor domain.Actor, preferences domain.UserPreferences) (domain.UserPreferences, error) {
	key, err := userKey(actor)
	if err != nil {
		return domain.UserPreferences{}, err
	}
	preferences = normalize(preferences)
	preferences.UpdatedAt = time.Now().UTC()
	if err := validate(preferences); err != nil {
		s.record(ctx, actor, err)
		return domain.UserPreferences{}, err
	}
	saved, err := s.repository.SaveUserPreferences(ctx, key, preferences)
	s.record(ctx, actor, err)
	return saved, err
}

func userKey(actor domain.Actor) (string, error) {
	username := strings.TrimSpace(actor.Username)
	if username == "" {
		return "", fmt.Errorf("%w: authenticated username is required", domain.ErrForbidden)
	}
	sum := sha256.Sum256([]byte(username))
	return hex.EncodeToString(sum[:12]), nil
}

func normalize(preferences domain.UserPreferences) domain.UserPreferences {
	if preferences.Favorites == nil {
		preferences.Favorites = []string{}
	}
	if preferences.Recent == nil {
		preferences.Recent = []domain.RecentResourcePreference{}
	}
	if preferences.SavedViews == nil {
		preferences.SavedViews = map[string][]domain.SavedResourceViewPreference{}
	}
	if preferences.HiddenColumns == nil {
		preferences.HiddenColumns = map[string][]string{}
	}
	return preferences
}

func validate(preferences domain.UserPreferences) error {
	encoded, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("%w: preferences cannot be encoded", domain.ErrInvalid)
	}
	if len(encoded) > maxPreferencesBytes {
		return fmt.Errorf("%w: preferences exceed 128 KiB", domain.ErrInvalid)
	}
	if len(preferences.Favorites) > 100 {
		return fmt.Errorf("%w: at most 100 favorite resource types are allowed", domain.ErrInvalid)
	}
	for _, value := range preferences.Favorites {
		if err := bounded(value, 253, "favorite"); err != nil {
			return err
		}
	}
	if len(preferences.Recent) > 50 {
		return fmt.Errorf("%w: at most 50 recent resources are allowed", domain.ErrInvalid)
	}
	for _, item := range preferences.Recent {
		if !strings.HasPrefix(item.Path, "/") || strings.HasPrefix(item.Path, "//") {
			return fmt.Errorf("%w: recent resource path must be a local absolute path", domain.ErrInvalid)
		}
		for label, value := range map[string]string{"recent path": item.Path, "recent title": item.Title, "recent subtitle": item.Subtitle, "recent kind": item.Kind, "recent namespace": item.Namespace} {
			limit := 1024
			if label == "recent kind" || label == "recent namespace" {
				limit = 253
			}
			if err := bounded(value, limit, label); err != nil {
				return err
			}
		}
	}
	if len(preferences.SavedViews) > 100 {
		return fmt.Errorf("%w: at most 100 saved-view resource keys are allowed", domain.ErrInvalid)
	}
	for key, views := range preferences.SavedViews {
		if err := bounded(key, 253, "saved-view resource key"); err != nil {
			return err
		}
		if len(views) > 12 {
			return fmt.Errorf("%w: at most 12 saved views per resource type are allowed", domain.ErrInvalid)
		}
		for _, view := range views {
			for label, value := range map[string]string{"saved-view id": view.ID, "saved-view name": view.Name, "saved-view query": view.Q, "saved-view namespace": view.Namespace} {
				limit := 253
				if label == "saved-view query" {
					limit = 1024
				}
				if err := bounded(value, limit, label); err != nil {
					return err
				}
			}
			if len(view.HiddenColumns) > 64 {
				return fmt.Errorf("%w: at most 64 hidden columns are allowed", domain.ErrInvalid)
			}
			for _, column := range view.HiddenColumns {
				if err := bounded(column, 253, "hidden column"); err != nil {
					return err
				}
			}
		}
	}
	if len(preferences.HiddenColumns) > 100 {
		return fmt.Errorf("%w: at most 100 hidden-column resource keys are allowed", domain.ErrInvalid)
	}
	for key, columns := range preferences.HiddenColumns {
		if err := bounded(key, 253, "hidden-column resource key"); err != nil {
			return err
		}
		if len(columns) > 64 {
			return fmt.Errorf("%w: at most 64 hidden columns are allowed", domain.ErrInvalid)
		}
	}
	return nil
}

func bounded(value string, limit int, label string) error {
	if len([]rune(value)) > limit {
		return fmt.Errorf("%w: %s exceeds %d characters", domain.ErrInvalid, label, limit)
	}
	return nil
}

func (s *Service) record(ctx context.Context, actor domain.Actor, err error) {
	if s.auditor == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "save-user-preferences", Resource: "preferences", Result: result, Err: err})
}
