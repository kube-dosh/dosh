package domain

import "time"

// RecentResourcePreference is the bounded, non-sensitive resource navigation
// history synchronized across a user's devices.
type RecentResourcePreference struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	VisitedAt int64  `json:"visitedAt"`
}

type SavedResourceViewPreference struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Q             string   `json:"q,omitempty"`
	Namespace     string   `json:"namespace,omitempty"`
	HiddenColumns []string `json:"hiddenColumns,omitempty"`
	CreatedAt     int64    `json:"createdAt"`
}

// UserPreferences is deliberately limited to UI navigation and view state. It
// must never contain Secret values, command output, or arbitrary user blobs.
type UserPreferences struct {
	Favorites     []string                                 `json:"favorites,omitempty"`
	Recent        []RecentResourcePreference               `json:"recent,omitempty"`
	SavedViews    map[string][]SavedResourceViewPreference `json:"savedViews,omitempty"`
	HiddenColumns map[string][]string                      `json:"hiddenColumns,omitempty"`
	UpdatedAt     time.Time                                `json:"updatedAt,omitempty"`
}
