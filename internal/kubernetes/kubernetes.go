package kubernetes

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	goyaml "gopkg.in/yaml.v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

type K8s struct {
	// Kubernetes client
	client *dynamic.DynamicClient
	list   map[string][]*unstructured.Unstructured
	dryRun bool
	ctx    context.Context
	log    *zap.SugaredLogger
}

func NewK8s(ctx context.Context, log *zap.SugaredLogger, kubeContext string, dryRun bool) (*K8s, error) {
	KubeContext = kubeContext
	if ctx == nil {
		ctx = context.TODO()
	}
	log = log.Named("k8s").With("context", kubeContext)
	k := &K8s{ctx: ctx, log: log}
	k.list = make(map[string][]*unstructured.Unstructured)
	if dryRun {
		k.dryRun = true
		k.log.Info("Dry run enabled")
		return k, nil
	}

	config, err := GetKubeConfig()
	if err != nil {
		k.log.Errorw("failed to get k8s config", "error", err)
		return nil, errors.Wrap(err, "")
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		k.log.Errorw("failed to create k8s client", "error", err)
		return nil, errors.Wrap(err, "")
	}
	k.client = client

	return k, nil
}

func (k *K8s) ApplyKustomize(path string) error {
	kustomize := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fsys := filesys.MakeFsOnDisk()
	m, err := kustomize.Run(fsys, path)
	if err != nil {
		k.log.Errorw("failed to run kustomize", "error", err)
		return errors.Wrap(err, "")
	}
	if err != nil {
		k.log.Errorw("failed to run kustomize", "error", err)
		return errors.Wrap(err, "")
	}
	for _, r := range m.Resources() {
		if err := k.ApplyResource(r); err != nil {
			k.log.Errorw("failed to apply resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	return nil
}

func (k *K8s) ApplyResource(res *resource.Resource) error {
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	y, err := res.AsYAML()
	if err != nil {
		k.log.Errorw("failed to convert resource to yaml", "error", err)
		return errors.Wrap(err, "")
	}
	obj := &unstructured.Unstructured{}
	_, _, err = decoder.Decode(y, nil, obj)
	if err != nil {
		k.log.Errorw("failed to decode resource", "error", err)
		return errors.Wrap(err, "")
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
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", namespace, "kind", kind, "name", obj.GetName())
		return nil
	}
	k.log.Infow("Creating resource", "namespace", namespace, "kind", kind, "name", obj.GetName())
	if kind == "Namespace" || kind == "CustomResourceDefinition" || kind == "ClusterRole" || kind == "ClusterRoleBinding" {
		_, err := k.client.Resource(gvr).Create(k.ctx, obj, metav1.CreateOptions{})
		// ignore error if already exists
		if err != nil && strings.Contains(err.Error(), "already exists") {
			return nil
		} else if err != nil {
			k.log.Errorw("failed to create resource", "error", err)
			return errors.Wrap(err, "")
		}
		return nil
	}
	_, err = k.client.Resource(gvr).Namespace(namespace).Create(k.ctx, obj, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	} else if err != nil {
		k.log.Errorw("failed to create resource", "error", err, "namespace", namespace, "kind", kind)
		return errors.Wrap(err, "")
	}

	return nil
}

func (k *K8s) CreateArgoInit(path, user, password string) error {
	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "infra-repo",
				"namespace": "argocd",
				"annotations": map[string]interface{}{
					"managed-by": "argocd.argoproj.io",
				},
				"labels": map[string]interface{}{
					"argocd.argoproj.io/secret-type": "repository",
				},
			},
			"type": "Opaque",
			"data": map[string]interface{}{
				"insecure": base64.StdEncoding.EncodeToString([]byte("true")),
				"name":     base64.StdEncoding.EncodeToString([]byte("infra")),
				"username": base64.StdEncoding.EncodeToString([]byte(user)),
				"password": base64.StdEncoding.EncodeToString([]byte(password)),
				"project":  base64.StdEncoding.EncodeToString([]byte("default")),
				"type":     base64.StdEncoding.EncodeToString([]byte("git")),
				"url":      base64.StdEncoding.EncodeToString([]byte("https://gitea.default.svc/infra/infra")), // this is the internal url
			},
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "argocd", "kind", "Secret", "name", "infra-repo")
	} else {
		k.log.Infow("Creating resource", "namespace", "argocd", "kind", "Secret", "name", "infra-repo")
		_, err := k.client.Resource(gvr).Namespace("argocd").Create(k.ctx, repo, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Repo already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create repo resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	argo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]interface{}{
				"name":      "init",
				"namespace": "argocd",
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "argocd.argoproj.io",
					"app.kubernetes.io/instance":   "init",
				},
				"annotations": map[string]interface{}{
					"argocd.argoproj.io/manifest-generate-paths": ".", // this is the path to the kustomization.yaml
				},
			},
			"spec": map[string]interface{}{
				"destination": map[string]interface{}{
					"namespace": "argocd",
					"server":    "https://kubernetes.default.svc",
				},
				"project": "default",
				"source": map[string]interface{}{
					"path":           "init",
					"repoURL":        "https://gitea.default.svc/infra/infra",
					"targetRevision": "HEAD",
				},
				"syncPolicy": map[string]interface{}{
					"automated": map[string]interface{}{},
				},
			},
		},
	}
	k.list["argocd"] = []*unstructured.Unstructured{}
	k.list["argocd"] = append(k.list["argocd"], argo)
	gvr = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "argocd", "kind", "Application", "name", "init")
	} else {
		k.log.Infow("Creating resource", "namespace", "argocd", "kind", "Application", "name", "init")
		_, err := k.client.Resource(gvr).Namespace("argocd").Create(k.ctx, argo, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Argo already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create argo resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	apps := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "ApplicationSet",
			"metadata": map[string]interface{}{
				"name":      "init",
				"namespace": "argocd",
			},
			"spec": map[string]interface{}{
				"goTemplate":        true,
				"goTemplateOptions": []string{"missingkey=error"},
				"generators": []map[string]interface{}{
					{
						"list": map[string]interface{}{
							"elements": []map[string]interface{}{
								{
									"path": "init",
								},
								{
									"path": "gitea",
								},
								{
									"path": "argocd",
								},
								{
									"path": "cert-manager",
								},
								{
									"path": "postgres-operator",
								},
								{
									"path": "valkey-operator",
								},
								{
									"path": "gitea-operator",
								},
							},
						},
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "{{.path}}",
						"labels": map[string]interface{}{
							"app.kubernetes.io/managed-by": "argocd.argoproj.io",
							"app.kubernetes.io/instance":   "{{.path}}",
						},
						"annotations": map[string]interface{}{
							"argocd.argoproj.io/manifest-generate-paths": ".", // this is the path to the kustomization.yaml
						},
					},
					"spec": map[string]interface{}{
						"destination": map[string]interface{}{
							"namespace": "argocd",
							"server":    "https://kubernetes.default.svc",
						},
						"project": "default",
						"source": map[string]interface{}{
							"path":           "{{.path}}",
							"repoURL":        "https://gitea.default.svc/infra/infra",
							"targetRevision": "HEAD",
						},
						"syncPolicy": map[string]interface{}{
							"automated": map[string]interface{}{},
						},
					},
				},
			},
		},
	}
	k.list["argocd"] = append(k.list["argocd"], apps)
	return nil
}

func (k *K8s) GetPivotPassword() (string, error) {
	secret, err := k.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).Namespace("default").Get(k.ctx, "pivot-password", metav1.GetOptions{})
	if err != nil {
		k.log.Errorw("failed to get secret", "error", err)
		return "", errors.Wrap(err, "")
	}
	pass, _, err := unstructured.NestedString(secret.Object, "data", "password")
	if err != nil {
		k.log.Errorw("failed to get password", "error", err)
		return "", errors.Wrap(err, "")
	}
	passb64, err := base64.StdEncoding.DecodeString(pass)
	if err != nil {
		k.log.Errorw("failed to decode password", "error", err)
		return "", errors.Wrap(err, "")
	}
	return string(passb64), nil
}

func (k *K8s) CreateGitea(path, user, password, domain string, valkey bool) error {
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
				"valkey":     valkey,
				"certIssuer": "selfsigned",
				"ingress": map[string]interface{}{
					"host": domain,
				},
			},
		},
	}
	k.list["gitea"] = []*unstructured.Unstructured{}
	k.list["gitea"] = append(k.list["gitea"], gitea)
	gvr := schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "gitea",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "default", "kind", "Gitea", "name", "gitea")
	} else {
		k.log.Infow("Creating resource", "namespace", "default", "kind", "Gitea", "name", "gitea")
		_, err := k.client.Resource(gvr).Namespace("default").Create(k.ctx, gitea, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Gitea already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create gitea resource", "error", err)
			return errors.Wrap(err, "")
		}
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
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "default", "kind", "Secret", "name", user+"-password")
	} else {
		k.log.Infow("Creating resource", "namespace", "default", "kind", "Secret", "name", user+"-password")
		_, err := k.client.Resource(gvr).Namespace("default").Create(k.ctx, passwordSecret, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Password already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create password resource", "error", err)
			return errors.Wrap(err, "")
		}
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
	k.list["gitea"] = append(k.list["gitea"], giteaUser)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "users",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "default", "kind", "User", "name", user)
	} else {
		k.log.Infow("Creating resource", "namespace", "default", "kind", "User", "name", user)
		_, err := k.client.Resource(gvr).Namespace("default").Create(k.ctx, giteaUser, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("User already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create user resource", "error", err)
			return errors.Wrap(err, "")
		}
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
	k.list["gitea"] = append(k.list["gitea"], org)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "orgs",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "default", "kind", "Org", "name", "infra")
	} else {
		k.log.Infow("Creating resource", "namespace", "default", "kind", "Org", "name", "infra")
		_, err := k.client.Resource(gvr).Namespace("default").Create(k.ctx, org, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Org already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create org resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hyperspike.io/v1",
			"kind":       "Repo",
			"metadata": map[string]interface{}{
				"name":      "infra",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"org": map[string]interface{}{
					"name": "infra",
				},
				"private": true,
			},
		},
	}
	k.list["gitea"] = append(k.list["gitea"], repo)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "repoes",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", "namespace", "default", "kind", "Repo", "name", "infra")
	} else {
		k.log.Infow("Creating resource", "namespace", "default", "kind", "Repo", "name", "infra")
		_, err := k.client.Resource(gvr).Namespace("default").Create(k.ctx, repo, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Repo already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create repo resource", "error", err)
			return errors.Wrap(err, "")
		}
	}
	return nil
}

func (k *K8s) WriteGiteaToFile(path string) error {
	if len(k.list["gitea"]) == 0 {
		k.log.Error("no objects to write, you may need to run CreateGitea first")
		return errors.New("no objects to write, you may need to run CreateGitea first")
	}
	return k.writeToFile("gitea", path)
}
func (k *K8s) WriteArgoToFile(path string) error {
	if len(k.list["argocd"]) == 0 {
		k.log.Error("no objects to write, you may need to run CreateArgoInit first")
		return errors.New("no objects to write, you may need to run CreateArgoInit first")
	}
	return k.writeToFile("argocd", path)
}

func (k *K8s) writeToFile(list, path string) error {
	if strings.Count(path, "/") > 1 {
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			k.log.Errorw("failed to create directory", "error", err)
			return errors.Wrap(err, "")
		}
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		k.log.Errorw("failed to create file", "error", err)
		return errors.Wrap(err, "")
	}
	defer func() {
		err := f.Close()
		if err != nil {
			k.log.Errorw("failed to close file", "error", err)
		}
	}()
	if len(k.list[list]) == 0 {
		k.log.Error("no objects to write, you may need to run CreateGitea first")
		return errors.New("no objects to write, you may need to run CreateGitea first")
	}
	for _, obj := range k.list[list] {
		y, err := goyaml.Marshal(obj.Object)
		if err != nil {
			k.log.Errorw("failed to marshal object", "error", err)
			return errors.Wrap(err, "")
		}
		if _, err = f.Write([]byte("---\n")); err != nil {
			k.log.Errorw("failed to write to file", "error", err)
			return errors.Wrap(err, "")
		}
		if _, err = f.Write(y); err != nil {
			k.log.Errorw("failed to write to file", "error", err)
			return errors.Wrap(err, "")
		}
	}
	return nil
}

var KubeContext string = ""

func includes(list []string, item string) bool {
	for _, i := range list {
		if i == item {
			return true
		}
	}
	return false
}

func fetchKubeConfig() (*clientcmdapi.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	if KubeContext != "" {
		if includes(maps.Keys(cfg.Contexts), KubeContext) {
			cfg.CurrentContext = KubeContext
		} else {
			return nil, fmt.Errorf("context %s not found in kubeconfig", KubeContext)
		}
	}
	return cfg, nil
}

func GetKubeConfig() (*rest.Config, error) {
	return clientcmd.BuildConfigFromKubeconfigGetter("", fetchKubeConfig)
}
