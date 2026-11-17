package domain

import "testing"

func TestParseImageReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		value            string
		registry         string
		repository       string
		tag              string
		explicitRegistry bool
	}{
		{name: "implicit docker hub", value: "nginx:alpine", registry: "docker.io", repository: "nginx", tag: "alpine"},
		{name: "docker hub namespace", value: "team/app:1.2", registry: "docker.io", repository: "team/app", tag: "1.2"},
		{name: "explicit registry", value: "registry.example.com/team/app:1.2.3", registry: "registry.example.com", repository: "team/app", tag: "1.2.3", explicitRegistry: true},
		{name: "registry port", value: "192.168.122.10:5000/dosh/dosh:latest", registry: "192.168.122.10:5000", repository: "dosh/dosh", tag: "latest", explicitRegistry: true},
		{name: "localhost", value: "localhost:5000/app", registry: "localhost:5000", repository: "app", tag: "latest", explicitRegistry: true},
		{name: "explicit registry without dot", value: "registry:5000/team/app:1", registry: "registry:5000", repository: "team/app", tag: "1", explicitRegistry: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reference, err := ParseImageReference(test.value)
			if err != nil {
				t.Fatalf("ParseImageReference() error = %v", err)
			}
			if reference.Registry != test.registry || reference.Repository != test.repository || reference.Tag != test.tag || reference.ExplicitRegistry != test.explicitRegistry {
				t.Fatalf("ParseImageReference() = %#v", reference)
			}
		})
	}
}

func TestParseImageReferenceWithDigest(t *testing.T) {
	t.Parallel()
	reference, err := ParseImageReference("ghcr.io/team/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("ParseImageReference() error = %v", err)
	}
	if reference.Registry != "ghcr.io" || reference.Repository != "team/app" || reference.Digest == "" || !reference.ExplicitRegistry {
		t.Fatalf("ParseImageReference() = %#v", reference)
	}
}

func TestParseImageReferenceRejectsInvalidImages(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "https://registry.example.com/app:1", "Bad/Image:1", "registry.example.com/app:", "registry.example.com/app@sha256:short", "app:bad tag"} {
		if _, err := ParseImageReference(value); err == nil {
			t.Fatalf("ParseImageReference(%q) error = nil", value)
		}
	}
}
