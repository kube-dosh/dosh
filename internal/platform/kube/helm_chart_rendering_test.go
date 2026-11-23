package kube

import "testing"

func TestRemoveHelmNotes(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"valkey/templates/NOTES.txt":       "NOTES:\nGet the password with:\n  export PASSWORD=$(kubectl get secret ...)\n",
		"NOTES.txt":                        "root notes",
		"valkey/templates/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\n",
	}
	removeHelmNotes(files)
	if _, ok := files["valkey/templates/NOTES.txt"]; ok {
		t.Fatal("namespaced NOTES.txt was not removed")
	}
	if _, ok := files["NOTES.txt"]; ok {
		t.Fatal("root NOTES.txt was not removed")
	}
	if files["valkey/templates/deployment.yaml"] == "" {
		t.Fatal("regular rendered template was removed")
	}
}
