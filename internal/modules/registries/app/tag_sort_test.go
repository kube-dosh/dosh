package app

import (
	"reflect"
	"testing"

	"dosh/internal/domain"
)

func TestSortTagsDescendingUsesVersionAwareNaturalOrder(t *testing.T) {
	t.Parallel()
	tags := []domain.ContainerRegistryTag{
		{Name: "0.1.9"},
		{Name: "latest"},
		{Name: "1.2.0-rc.2"},
		{Name: "0.1.10"},
		{Name: "debug"},
		{Name: "1.2.0"},
		{Name: "0.1.8"},
		{Name: "1.2.0-rc.10"},
		{Name: "v1.3.0"},
		{Name: "nightly"},
		{Name: "1.2.0-20260101"},
	}

	sortTagsDescending(tags)

	got := make([]string, 0, len(tags))
	for _, tag := range tags {
		got = append(got, tag.Name)
	}
	want := []string{
		"v1.3.0",
		"1.2.0",
		"1.2.0-rc.10",
		"1.2.0-rc.2",
		"1.2.0-20260101",
		"0.1.10",
		"0.1.9",
		"0.1.8",
		"nightly",
		"latest",
		"debug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted tags = %#v, want %#v", got, want)
	}
}
