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

const (
	APIVERSION = "apiVersion"
	KIND       = "kind"
	METADATA   = "metadata"
	NAME       = "name"
	NAMESPACE  = "namespace"
	ARGOCD     = "argocd"
	INIT       = "init"
	DEFAULT    = "default"
	PATH       = "path"
	SPEC       = "spec"
	GITEA      = "gitea"
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

func (k *K8s) CreateNamespace(namespace string) error {
	ns := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "v1",
			KIND:       "Namespace",
			METADATA: map[string]interface{}{
				NAME: namespace,
			},
		},
	}
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", KIND, "Namespace", NAME, namespace)
		return nil
	}
	k.log.Infow("Creating resource", KIND, "Namespace", NAME, namespace)
	_, err := k.client.Resource(gvr).Create(k.ctx, ns, metav1.CreateOptions{})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	} else if err != nil {
		k.log.Errorw("failed to create resource", "error", err)
		return errors.Wrap(err, "")
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
		k.log.Infow("Dry run: Creating resource", NAMESPACE, namespace, KIND, kind, NAME, obj.GetName())
		return nil
	}
	k.log.Infow("Creating resource", NAMESPACE, namespace, KIND, kind, NAME, obj.GetName())
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
		k.log.Errorw("failed to create resource", "error", err, NAMESPACE, namespace, KIND, kind)
		return errors.Wrap(err, "")
	}

	return nil
}

func (k *K8s) CreateArgoInit(path, user, password string) error {
	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "v1",
			KIND:       "Secret",
			METADATA: map[string]interface{}{
				NAME:      "infra-repo",
				NAMESPACE: ARGOCD,
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
				NAME:       base64.StdEncoding.EncodeToString([]byte("infra")),
				"username": base64.StdEncoding.EncodeToString([]byte(user)),
				"password": base64.StdEncoding.EncodeToString([]byte(password)),
				"project":  base64.StdEncoding.EncodeToString([]byte(DEFAULT)),
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
		k.log.Infow("Dry run: Creating resource", NAMESPACE, ARGOCD, KIND, "Secret", NAME, "infra-repo")
	} else {
		k.log.Infow("Creating resource", NAMESPACE, ARGOCD, KIND, "Secret", NAME, "infra-repo")
		_, err := k.client.Resource(gvr).Namespace(ARGOCD).Create(k.ctx, repo, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Repo already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create repo resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	argo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "argoproj.io/v1alpha1",
			KIND:       "Application",
			METADATA: map[string]interface{}{
				NAME:      INIT,
				NAMESPACE: ARGOCD,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "argocd.argoproj.io",
					"app.kubernetes.io/instance":   INIT,
				},
				"annotations": map[string]interface{}{
					"argocd.argoproj.io/manifest-generate-paths": ".", // this is the path to the kustomization.yaml
				},
			},
			SPEC: map[string]interface{}{
				"destination": map[string]interface{}{
					NAMESPACE: ARGOCD,
					"server":  "https://kubernetes.default.svc",
				},
				"project": DEFAULT,
				"source": map[string]interface{}{
					PATH:             INIT,
					"repoURL":        "https://gitea.default.svc/infra/infra",
					"targetRevision": "HEAD",
				},
				"syncPolicy": map[string]interface{}{
					"automated": map[string]interface{}{},
				},
			},
		},
	}
	k.list[ARGOCD] = []*unstructured.Unstructured{}
	k.list[ARGOCD] = append(k.list[ARGOCD], argo)
	gvr = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", NAMESPACE, ARGOCD, KIND, "Application", NAME, INIT)
	} else {
		k.log.Infow("Creating resource", NAMESPACE, ARGOCD, KIND, "Application", NAME, INIT)
		_, err := k.client.Resource(gvr).Namespace(ARGOCD).Create(k.ctx, argo, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Argo already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create argo resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	apps := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "argoproj.io/v1alpha1",
			KIND:       "ApplicationSet",
			METADATA: map[string]interface{}{
				NAME:      INIT,
				NAMESPACE: ARGOCD,
			},
			SPEC: map[string]interface{}{
				"goTemplate":        true,
				"goTemplateOptions": []string{"missingkey=error"},
				"generators": []map[string]interface{}{
					{
						"list": map[string]interface{}{
							"elements": []map[string]interface{}{
								{
									PATH: INIT,
								},
								{
									PATH: GITEA,
								},
								{
									PATH: ARGOCD,
								},
								{
									PATH: "cert-manager",
								},
								{
									PATH: "postgres-operator",
								},
								{
									PATH: "valkey-operator",
								},
								{
									PATH: "gitea-operator",
								},
							},
						},
					},
				},
				"template": map[string]interface{}{
					METADATA: map[string]interface{}{
						NAME: "{{.path}}",
						"labels": map[string]interface{}{
							"app.kubernetes.io/managed-by": "argocd.argoproj.io",
							"app.kubernetes.io/instance":   "{{.path}}",
						},
						"annotations": map[string]interface{}{
							"argocd.argoproj.io/manifest-generate-paths": ".", // this is the path to the kustomization.yaml
						},
					},
					SPEC: map[string]interface{}{
						"destination": map[string]interface{}{
							NAMESPACE: ARGOCD,
							"server":  "https://kubernetes.default.svc",
						},
						"project": DEFAULT,
						"source": map[string]interface{}{
							PATH:             "{{.path}}",
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
	k.list[ARGOCD] = append(k.list[ARGOCD], apps)
	return nil
}

func (k *K8s) GetPivotPassword() (string, error) {
	secret, err := k.client.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).Namespace(DEFAULT).Get(k.ctx, "pivot-password", metav1.GetOptions{})
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
			APIVERSION: "hyperspike.io/v1",
			KIND:       "Gitea",
			METADATA: map[string]interface{}{
				NAME:      GITEA,
				NAMESPACE: DEFAULT,
			},
			SPEC: map[string]interface{}{
				"tls":        true,
				"valkey":     valkey,
				"certIssuer": "selfsigned",
				"ingress": map[string]interface{}{
					"host": domain,
				},
			},
		},
	}
	k.list[GITEA] = []*unstructured.Unstructured{}
	k.list[GITEA] = append(k.list[GITEA], gitea)
	gvr := schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: GITEA,
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", NAMESPACE, DEFAULT, KIND, "Gitea", NAME, GITEA)
	} else {
		k.log.Infow("Creating resource", NAMESPACE, DEFAULT, KIND, "Gitea", NAME, GITEA)
		_, err := k.client.Resource(gvr).Namespace(DEFAULT).Create(k.ctx, gitea, metav1.CreateOptions{})
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
			APIVERSION: "v1",
			KIND:       "Secret",
			METADATA: map[string]interface{}{
				NAME:      user + "-password",
				NAMESPACE: DEFAULT,
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
		k.log.Infow("Dry run: Creating resource", NAMESPACE, DEFAULT, KIND, "Secret", NAME, user+"-password")
	} else {
		k.log.Infow("Creating resource", NAMESPACE, DEFAULT, KIND, "Secret", NAME, user+"-password")
		_, err := k.client.Resource(gvr).Namespace(DEFAULT).Create(k.ctx, passwordSecret, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Password already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create password resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	giteaUser := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "hyperspike.io/v1",
			KIND:       "User",
			METADATA: map[string]interface{}{
				NAME:      user,
				NAMESPACE: DEFAULT,
			},
			SPEC: map[string]interface{}{
				"email": fmt.Sprintf("%s@%s", user, domain),
				"password": map[string]interface{}{
					NAME:  user + "-password",
					"key": "password",
				},
				"instance": map[string]interface{}{
					NAME: GITEA,
				},
			},
		},
	}
	k.list[GITEA] = append(k.list[GITEA], giteaUser)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "users",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", NAMESPACE, DEFAULT, KIND, "User", NAME, user)
	} else {
		k.log.Infow("Creating resource", NAMESPACE, DEFAULT, KIND, "User", NAME, user)
		_, err := k.client.Resource(gvr).Namespace(DEFAULT).Create(k.ctx, giteaUser, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("User already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create user resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	org := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "hyperspike.io/v1",
			KIND:       "Org",
			METADATA: map[string]interface{}{
				NAME:      "infra",
				NAMESPACE: DEFAULT,
			},
			SPEC: map[string]interface{}{
				"description": "Infrastructure team",
				"instance": map[string]interface{}{
					NAME: GITEA,
				},
				"teams": []map[string]interface{}{
					{
						NAME:              "admin",
						"permission":      "admin",
						"includeAllRepos": true,
						"createOrgRepo":   true,
						"members":         []string{user},
					},
				},
			},
		},
	}
	k.list[GITEA] = append(k.list[GITEA], org)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "orgs",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", NAMESPACE, DEFAULT, KIND, "Org", NAME, "infra")
	} else {
		k.log.Infow("Creating resource", NAMESPACE, DEFAULT, KIND, "Org", NAME, "infra")
		_, err := k.client.Resource(gvr).Namespace(DEFAULT).Create(k.ctx, org, metav1.CreateOptions{})
		if err != nil && strings.Contains(err.Error(), "already exists") {
			k.log.Infow("Org already exists, ignoring")
		} else if err != nil {
			k.log.Errorw("failed to create org resource", "error", err)
			return errors.Wrap(err, "")
		}
	}

	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			APIVERSION: "hyperspike.io/v1",
			KIND:       "Repo",
			METADATA: map[string]interface{}{
				NAME:      "infra",
				NAMESPACE: DEFAULT,
			},
			SPEC: map[string]interface{}{
				"org": map[string]interface{}{
					NAME: "infra",
				},
				"private": true,
			},
		},
	}
	k.list[GITEA] = append(k.list[GITEA], repo)
	gvr = schema.GroupVersionResource{
		Group:    "hyperspike.io",
		Version:  "v1",
		Resource: "repoes",
	}
	if k.dryRun {
		k.log.Infow("Dry run: Creating resource", NAMESPACE, DEFAULT, KIND, "Repo", NAME, "infra")
	} else {
		k.log.Infow("Creating resource", NAMESPACE, DEFAULT, KIND, "Repo", NAME, "infra")
		_, err := k.client.Resource(gvr).Namespace(DEFAULT).Create(k.ctx, repo, metav1.CreateOptions{})
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
	if len(k.list[GITEA]) == 0 {
		k.log.Error("no objects to write, you may need to run CreateGitea first")
		return errors.New("no objects to write, you may need to run CreateGitea first")
	}
	return k.writeToFile(GITEA, path)
}
func (k *K8s) WriteArgoToFile(path string) error {
	if len(k.list[ARGOCD]) == 0 {
		k.log.Error("no objects to write, you may need to run CreateArgoInit first")
		return errors.New("no objects to write, you may need to run CreateArgoInit first")
	}
	return k.writeToFile(ARGOCD, path)
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
