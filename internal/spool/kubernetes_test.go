package spool

import (
	"os"
	"testing"
)

func TestKubeContext(t *testing.T) {
	KubeContext = "does-not-exist"

	_, err := GetKubeConfig()
	if err == nil {
		t.Errorf("Expected error when kube context does not exist")
	}
	os.Setenv("KUBECONFIG", "testdata/kubeconfig")
	KubeContext = "default"
	if err := os.Mkdir("testdata", 0755); err != nil {
		t.Errorf("Error creating testdata directory %v", err)
	}
	// Create a kubeconfig file in the testdata directory
	file, err := os.Create("testdata/kubeconfig")
	if err != nil {
		t.Errorf("Error creating kubeconfig file %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Error closing kubeconfig file %v", err)
		}
		os.Remove("testdata/kubeconfig")
		os.Remove("testdata")
	}()
	if _, err := file.WriteString(`apiVersion: v1
clusters:
- cluster:
    server: https://localhost:6443
  name: default
- cluster:
     server: https://minikube:8443
  name: minikube
contexts:
- context:
    cluster: default
    user: default
  name: default
- context:
    cluster: minikube
    user: minikube
  name: minikube
current-context: default
kind: Config
preferences: {}
users:
- name: default
- name: minikube
`); err != nil {
		t.Errorf("Error writing to kubeconfig file %v", err)
	}
	KubeContext = "default"

	_, err = GetKubeConfig()
	if err != nil {
		t.Errorf("Error getting [default] kubeconfig %v", err)
	}
	KubeContext = "minikube"

	cfg, err := GetKubeConfig()
	if err != nil {
		t.Errorf("Error getting [minikube] kubeconfig %v", err)
	}
	if cfg.Host != "https://minikube:8443" {
		t.Errorf("Expected host to be https://minikube:8443, got %v", cfg.Host)
	}
}
