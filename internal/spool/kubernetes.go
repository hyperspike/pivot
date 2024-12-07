package spool

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goyaml "gopkg.in/yaml.v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

type K8s struct {
	// Kubernetes client
	client *dynamic.DynamicClient
}

func NewK8s() (*K8s, error) {
	config, err := getK8sConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s config: %w", err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &K8s{client: client}, nil
}

func (k *K8s) ApplyKustomize(path string) error {
	kustomize := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fsys := filesys.MakeFsOnDisk()
	m, err := kustomize.Run(fsys, path)
	if err != nil {
		return fmt.Errorf("failed to run kustomize: %w", err)
	}
	if err != nil {
		return fmt.Errorf("failed to convert kustomize to yaml: %w", err)
	}
	for _, r := range m.Resources() {
		if err := k.ApplyResource(r); err != nil {
			return fmt.Errorf("failed to apply resource: %w", err)
		}
	}

	return nil
}

func (k *K8s) ApplyResource(res *resource.Resource) error {
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	y, err := res.AsYAML()
	if err != nil {
		return fmt.Errorf("failed to convert resource to yaml: %w", err)
	}
	obj := &unstructured.Unstructured{}
	_, _, err = decoder.Decode(y, nil, obj)
	if err != nil {
		return fmt.Errorf("failed to decode resource: %w", err)
	}
	namespace := res.GetNamespace()
	resource := strings.ToLower(res.GetGvk().Kind)
	if strings.HasSuffix(resource, "y") {
		resource = strings.TrimSuffix(resource, "y") + "ie"
	}
	resource = resource + "s"

	gvr := schema.GroupVersionResource{
		Group:    obj.GroupVersionKind().Group,
		Version:  obj.GroupVersionKind().Version,
		Resource: resource,
	}
	kind := obj.GetKind()
	fmt.Printf("Creating %v\n", gvr)
	if kind == "Namespace" || kind == "CustomResourceDefinition" || kind == "ClusterRole" || kind == "ClusterRoleBinding" {
		_, err := k.client.Resource(gvr).Create(context.TODO(), obj, metav1.CreateOptions{})
		// ignore error if already exists
		if err != nil && strings.Contains(err.Error(), "already exists") {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to create kind [%s] resource: %w", kind, err)
		}
		return nil
	}
	_, err = k.client.Resource(gvr).Namespace(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	} else if err != nil {
		fmt.Println(string(y))
		return fmt.Errorf("failed to create ns [%s] kind [%s] resource: %w", namespace, kind, err)
	}

	return nil
}

func (k *K8s) CreateGitea(path, user, password, domain string) error {
	list := []*unstructured.Unstructured{}
	gitea := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hyperspike.io/v1",
			"kind":       "Gitea",
			"metadata": map[string]interface{}{
				"name":      "gitea",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"tls":        true,
				"valkey":     true,
				"certIssuer": "selfsigned",
				"ingress": map[string]interface{}{
					"host": "git.local.net",
				},
			},
		},
	}
	list = append(list, gitea)
	gvr := schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "gitea",
	}
	fmt.Printf("Creating %v\n", gvr)
	_, err := k.client.Resource(gvr).Namespace("default").Create(context.TODO(), gitea, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		fmt.Println("Gitea already exists, ignoring")
	} else if err != nil {
		return fmt.Errorf("failed to create gitea resource: %w", err)
	}

	base64pass := base64.StdEncoding.EncodeToString([]byte(password))

	passwordSecret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      user + "-password",
				"namespace": "default",
			},
			"type": "Opaque",
			"data": map[string]interface{}{
				"password": base64pass,
			},
		},
	}
	gvr = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}
	fmt.Printf("Creating %v\n", gvr)
	_, err = k.client.Resource(gvr).Namespace("default").Create(context.TODO(), passwordSecret, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		fmt.Println("Password already exists, ignoring")
	} else if err != nil {
		return fmt.Errorf("failed to create password resource: %w", err)
	}

	giteaUser := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hyperspike.io/v1",
			"kind":       "User",
			"metadata": map[string]interface{}{
				"name":      user,
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"email": fmt.Sprintf("%s@%s", user, domain),
				"password": map[string]interface{}{
					"name": user + "-password",
					"key":  "password",
				},
				"instance": map[string]interface{}{
					"name": "gitea",
				},
			},
		},
	}
	list = append(list, giteaUser)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "users",
	}
	fmt.Printf("Creating %v\n", gvr)
	_, err = k.client.Resource(gvr).Namespace("default").Create(context.TODO(), giteaUser, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		fmt.Println("User already exists, ignoring")
	} else if err != nil {
		return fmt.Errorf("failed to create user resource: %w", err)
	}

	org := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hyperspike.io/v1",
			"kind":       "Org",
			"metadata": map[string]interface{}{
				"name":      "infra",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"description": "Infrastructure team",
				"instance": map[string]interface{}{
					"name": "gitea",
				},
				"teams": []map[string]interface{}{
					{
						"name":            "admin",
						"permission":      "admin",
						"includeAllRepos": true,
						"createOrgRepo":   true,
						"members":         []string{user},
					},
				},
			},
		},
	}
	list = append(list, org)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "orgs",
	}
	fmt.Printf("Creating %v\n", gvr)
	_, err = k.client.Resource(gvr).Namespace("default").Create(context.TODO(), org, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		fmt.Println("Org already exists, ignoring")

	} else if err != nil {
		return fmt.Errorf("failed to create org resource: %w", err)
	}
	writeToFile(filepath.Join("infra", "gitea", "gitea.yaml"), list)
	return nil
}

func writeToFile(path string, list []*unstructured.Unstructured) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()
	for _, obj := range list {
		y, err := goyaml.Marshal(obj.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal object: %w", err)
		}
		if _, err = f.Write([]byte("---\n")); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
		if _, err = f.Write(y); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
	}
	return nil
}

func getK8sConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
