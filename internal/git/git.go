package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

type Spool struct {
	Repo   *git.Repository
	Path   string
	Remote string
	ctx    context.Context
	log    *zap.SugaredLogger
}

var (
	Email = "pivot@hyperspike.io"
	Name  = "Pivot GitOps"
)

func RepoExists(path string) bool {
	exists := false
	_, err := git.PlainOpen(path)
	if err == nil {
		exists = true
	}
	return exists
}

// Create a new git repository and adds the initial GitOps tooling
func CreateRepo(ctx context.Context, log *zap.SugaredLogger, path string) (*Spool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	repo, err := git.PlainInitWithOptions(path, &git.PlainInitOptions{
		Bare: false,
		InitOptions: git.InitOptions{
			DefaultBranch: plumbing.Main,
		},
	})
	log = log.Named("git").With("path", path)
	if err != nil {
		log.Errorw("failed to create git repo", "error", err)
		return nil, err
	}
	s := &Spool{
		Path: path,
		Repo: repo,
		ctx:  ctx,
		log:  log,
	}
	if err = s.readme(); err != nil {
		return nil, err
	}
	if err = s.addUrl(
		"https://raw.githubusercontent.com/argoproj/argo-cd/refs/heads/master/manifests/install.yaml",
		"argocd/argocd.yaml",
		"adding argo-cd"); err != nil {
		return nil, err
	}
	if err = s.addNamespace("argocd", "adding argo-cd namespace"); err != nil {
		s.log.Errorw("failed to add argo-cd namespace", "error", err)
		return nil, err
	}
	if err = s.createKustomization("argocd", "adding argo-cd kustomization"); err != nil {
		return nil, err
	}
	l, err := getLatest("https://api.github.com/repos/cert-manager/cert-manager/releases/latest")
	if err != nil {
		return nil, err
	}
	if err = s.addUrl(
		"https://github.com/cert-manager/cert-manager/releases/download/"+l+"/cert-manager.yaml",
		"cert-manager/cert-manager.yaml",
		"adding cert-manager"); err != nil {
		return nil, err
	}
	if err = s.createKustomization("cert-manager", "adding cert-manager kustomization"); err != nil {
		return nil, err
	}
	l, err = getLatest("https://api.github.com/repos/hyperspike/valkey-operator/releases/latest")
	if err != nil {
		return nil, err
	}
	if err = s.addUrl(
		"https://github.com/hyperspike/valkey-operator/releases/download/"+l+"/install.yaml",
		"valkey-operator/valkey-operator.yaml",
		"adding valkey-operator"); err != nil {
		return nil, err
	}
	if err = s.createKustomization("valkey-operator", "adding valkey kustomization"); err != nil {
		return nil, err
	}
	l, err = getLatest("https://api.github.com/repos/zalando/postgres-operator/releases/latest")
	if err != nil {
		return nil, err
	}
	if err := s.cloneTag(
		"https://github.com/zalando/postgres-operator",
		"postgres-operator",
		l,
	); err != nil {
		return nil, err
	}
	if err = s.concatFiles(
		[]string{
			"postgres-operator/manifests/configmap.yaml",
			"postgres-operator/manifests/operator-service-account-rbac.yaml",
			"postgres-operator/manifests/postgres-operator.yaml",
			"postgres-operator/manifests/api-service.yaml",
		},
		"postgres-operator/postgres-operator.yaml",
		"---\n",
		"adding postgres-operator"); err != nil {
		return nil, err
	}
	if err = s.createKustomization("postgres-operator", "adding postgres kustomization"); err != nil {
		return nil, err
	}
	l, err = getLatest("https://api.github.com/repos/hyperspike/gitea-operator/releases/latest")
	if err != nil {
		return nil, err
	}
	if err = s.addUrl(
		"https://github.com/hyperspike/gitea-operator/releases/download/"+l+"/install.yaml",
		"gitea-operator/gitea-operator.yaml",
		"adding gitea-operator"); err != nil {
		return nil, err
	}
	if err = s.createKustomization("gitea-operator", "adding gitea kustomization"); err != nil {
		return nil, err
	}
	return s, nil
}

// Add a README.md file to the repository
func (s *Spool) readme() error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	f := filepath.Join(s.Path, "README.md")
	if err = os.WriteFile(f, []byte("# Pivot GitOps"), 0600); err != nil {
		s.log.Errorw("failed to write README.md", "error", err)
		return err
	}
	if _, err = w.Add("README.md"); err != nil {
		s.log.Errorw("failed to add README.md", "error", err)
		return err
	}
	if err = s.commit("Initial commit"); err != nil {
		s.log.Errorw("failed to commit README.md", "error", err)
		return err
	}
	return nil
}

func (s *Spool) commit(msg string) error {
	s.log.Infow("committing", "message", msg)
	w, err := s.Repo.Worktree()
	if err != nil {
		s.log.Errorw("failed to get worktree", "error", err)
		return err
	}
	if _, err = w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  Name,
			Email: Email,
			When:  time.Now(),
		},
	}); err != nil {
		s.log.Errorw("failed to commit", "error", err)
		return err
	}
	return nil
}

func (s *Spool) AddRemote(name, remote string) error {
	if name == "" {
		name = "origin"
	}
	_, err := s.Repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{remote},
	})
	s.Remote = remote
	return err
}

func (s *Spool) PushBasic(remote, user, pass string) error {
	remoteObj, err := s.Repo.Remote(remote)
	if err != nil {
		s.log.Errorw("failed to get remote", "error", err, "remote", remote)
		return err
	}
	s.log.Infow("pushing", "remote", remote, "url", remoteObj.Config().URLs[0])
	auth := &githttp.BasicAuth{
		Username: user,
		Password: pass,
	}
	err = s.Repo.Push(&git.PushOptions{
		RemoteName:      remote,
		InsecureSkipTLS: true,
		Auth:            auth,
	})
	if err != nil {
		s.log.Errorw("failed to push", "error", err)
	}
	return err
}

func (s *Spool) AddExisting(path string) error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	if _, err = w.Add(filepath.Clean(path)); err != nil {
		return err
	}
	if err = s.commit("Adding " + path); err != nil {
		return err
	}
	return nil
}

func (s *Spool) concatFiles(files []string, filePath, separator, msg string) error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	f := filepath.Join(s.Path, filepath.Clean(filePath))
	if strings.Count(f, "/") > 1 {
		if err := os.MkdirAll(filepath.Dir(f), 0750); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(f, s.Path) {
		return fmt.Errorf("invalid file path %s", f)
	}
	fh, err := os.OpenFile(filepath.Clean(f), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	defer func() {
		if err := fh.Close(); err != nil {
			s.log.Errorw("error closing file", "error", err)
		}
	}()
	if err != nil {
		if err := fh.Sync(); err != nil {
			s.log.Errorw("error syncing file", "error", err)
		}
		return err
	}
	for _, file := range files {
		readBody, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return err
		}
		if _, err := fh.Write([]byte(separator)); err != nil {
			return err
		}
		if _, err := fh.Write(readBody); err != nil {
			return err
		}
	}
	if _, err = w.Add(filePath); err != nil {
		return err
	}
	if err = s.commit(msg); err != nil {
		return err
	}
	if err := fh.Sync(); err != nil {
		return err
	}
	return nil
}

func (s *Spool) addUrl(url, filePath, msg string) error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	readBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if err := res.Body.Close(); err != nil {
		return err
	}
	f := filepath.Join(s.Path, filePath)
	if strings.Count(f, "/") > 1 {
		if err := os.MkdirAll(filepath.Dir(f), 0750); err != nil {
			return err
		}
	}
	if err = os.WriteFile(f, readBody, 0600); err != nil {
		return err
	}
	s.log.Infow("added file", "file", f)
	if _, err = w.Add(filePath); err != nil {
		return err
	}
	if err = s.commit(msg); err != nil {
		return err
	}
	return nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func getLatest(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	readBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if err := res.Body.Close(); err != nil {
		return "", err
	}
	latest := githubRelease{}
	if err := json.Unmarshal(readBody, &latest); err != nil {
		return "", err
	}
	return latest.TagName, nil
}

func (s *Spool) cloneTag(url, path, tag string) error {
	if RepoExists(path) {
		return nil
	}
	_, err := git.PlainClone(path, false, &git.CloneOptions{
		URL:           url,
		Depth:         1,
		ReferenceName: plumbing.NewTagReferenceName(tag),
		SingleBranch:  true,
	})
	return err
}

func (s *Spool) addNamespace(path, msg string) error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	f := filepath.Join(s.Path, path, "namespace.yaml")
	if strings.Count(f, "/") > 1 {
		if err := os.MkdirAll(filepath.Dir(f), 0750); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(f, s.Path) {
		return fmt.Errorf("invalid file path %s", f)
	}
	fh, err := os.OpenFile(filepath.Clean(f), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() {
		err := fh.Close()
		if err != nil {
			s.log.Errorw("error closing file", "error", err)
		}
	}()
	if _, err = fh.Write([]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + path + "\n")); err != nil {
		return err
	}
	if _, err = w.Add(path + "/namespace.yaml"); err != nil {
		return err
	}
	if err = s.commit(msg); err != nil {
		return err
	}
	return nil
}

func dirIncludes(files []os.DirEntry, name string) bool {
	for _, file := range files {
		if file.Name() == name {
			return true
		}
	}
	return false
}

func (s *Spool) GenerateKustomize(namespace, path string) error {
	return s.createKustomizationWithNamespace(path, namespace, "adding "+path+" kustomization")
}

func (s *Spool) createKustomizationWithNamespace(path, namespace, msg string) error {
	w, err := s.Repo.Worktree()
	if err != nil {
		return err
	}
	fk := filepath.Join(s.Path, path, "kustomization.yaml")
	if strings.Count(fk, "/") > 1 {
		if err := os.MkdirAll(filepath.Dir(fk), 0750); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(fk, s.Path) {
		return fmt.Errorf("invalid file path %s", fk)
	}
	fhk, err := os.OpenFile(filepath.Clean(fk), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() {
		err := fhk.Close()
		if err != nil {
			s.log.Errorw("error closing file", "error", err)
		}
	}()
	if _, err = fhk.Write([]byte("namespace: " + namespace + "\nresources:\n")); err != nil {
		return err
	}
	files, err := os.ReadDir(filepath.Join(s.Path, path))
	if err != nil {
		return err
	}
	if dirIncludes(files, "namespace.yaml") {
		str := "- namespace.yaml\n"
		if _, err = fhk.Write([]byte(str)); err != nil {
			return err
		}
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if file.Name() == "kustomization.yaml" {
			continue
		}
		if file.Name() == "namespace.yaml" {
			continue
		}
		str := "- " + file.Name() + "\n"
		if _, err = fhk.Write([]byte(str)); err != nil {
			return err
		}
	}
	if _, err = w.Add(path + "/kustomization.yaml"); err != nil {
		return err
	}
	if err = s.commit(msg); err != nil {
		return err
	}
	return nil
}

func (s *Spool) createKustomization(path, msg string) error {
	return s.createKustomizationWithNamespace(path, path, msg)
}
