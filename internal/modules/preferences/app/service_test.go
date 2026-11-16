package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type fakePreferencesRepository struct {
	preferences domain.UserPreferences
	key         string
	saves       int
}

func (r *fakePreferencesRepository) GetUserPreferences(_ context.Context, key string) (domain.UserPreferences, error) {
	r.key = key
	return r.preferences, nil
}

func (r *fakePreferencesRepository) SaveUserPreferences(_ context.Context, key string, preferences domain.UserPreferences) (domain.UserPreferences, error) {
	r.key, r.preferences, r.saves = key, preferences, r.saves+1
	return preferences, nil
}

func TestPreferencesAreScopedByStableUserHash(t *testing.T) {
	repository := &fakePreferencesRepository{}
	service := New(repository, nil)
	first, err := service.Get(context.Background(), domain.Actor{Username: "alice@example.test"})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if first.Favorites == nil || first.Recent == nil || first.SavedViews == nil || first.HiddenColumns == nil {
		t.Fatalf("normalized preferences = %#v", first)
	}
	firstKey := repository.key
	if _, err := service.Get(context.Background(), domain.Actor{Username: "alice@example.test"}); err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	if repository.key != firstKey || len(firstKey) < 16 {
		t.Fatalf("user key changed or is too short: %q / %q", firstKey, repository.key)
	}
	if _, err := service.Get(context.Background(), domain.Actor{Username: "bob@example.test"}); err != nil {
		t.Fatalf("Get() bob error = %v", err)
	}
	if repository.key == firstKey {
		t.Fatal("different users share a preferences key")
	}
}

func TestPreferencesRejectUnsafeOrOversizedData(t *testing.T) {
	service := New(&fakePreferencesRepository{}, nil)
	actor := domain.Actor{Username: "alice"}
	if _, err := service.Save(context.Background(), actor, domain.UserPreferences{Recent: []domain.RecentResourcePreference{{Path: "https://evil.example", Title: "x"}}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("external path error = %v", err)
	}
	if _, err := service.Save(context.Background(), actor, domain.UserPreferences{Recent: []domain.RecentResourcePreference{{Path: "//evil.example", Title: "x"}}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("protocol-relative path error = %v", err)
	}
	recent := make([]domain.RecentResourcePreference, 51)
	for index := range recent {
		recent[index] = domain.RecentResourcePreference{Path: "/pods/default/pod", Title: "pod"}
	}
	if _, err := service.Save(context.Background(), actor, domain.UserPreferences{Recent: recent}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized recent error = %v", err)
	}
	if _, err := service.Save(context.Background(), domain.Actor{}, domain.UserPreferences{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("anonymous error = %v", err)
	}
}

func TestPreferencesSaveSetsUpdatedAt(t *testing.T) {
	repository := &fakePreferencesRepository{}
	service := New(repository, nil)
	saved, err := service.Save(context.Background(), domain.Actor{Username: "alice"}, domain.UserPreferences{Favorites: []string{"_core/pods"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.UpdatedAt.IsZero() || repository.saves != 1 || len(saved.Favorites) != 1 {
		t.Fatalf("saved = %#v saves=%d", saved, repository.saves)
	}
}
