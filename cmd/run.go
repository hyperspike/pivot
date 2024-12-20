package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"hyperspike.io/pivot/internal/git"
	"hyperspike.io/pivot/internal/kubernetes"
	"hyperspike.io/pivot/internal/proxy"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "start pivoting",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.TODO()
		log := getLogger(cmd)
		r, err := git.CreateRepo(ctx, log, "infra")
		if err != nil {
			return
		}
		dryRun := cmd.Flag("dry-run").Value.String() == "true"
		k8s, err := kubernetes.NewK8s(ctx, log, cmd.Flag("context").Value.String(), dryRun)
		if err != nil {
			log.Fatalw("failed to create k8s", "error", err)
		}
		if err := k8s.ApplyKustomize("infra/cert-manager"); err != nil {
			log.Fatalw("failed to apply cert-manager", "error", err)
		}
		if err := k8s.ApplyKustomize("infra/argocd"); err != nil {
			log.Fatalw("failed to apply argocd", "error", err)
		}
		if err := k8s.ApplyKustomize("infra/postgres-operator"); err != nil {
			log.Fatalw("failed to apply postgres-operator", "error", err)
		}
		if err := k8s.ApplyKustomize("infra/valkey-operator"); err != nil {
			log.Fatalw("failed to apply valkey-operator", "error", err)
		}
		if err := k8s.ApplyKustomize("infra/gitea-operator"); err != nil {
			log.Fatalw("failed to apply gitea-operator", "error", err)
		}
		pass := cmd.Flag("password").Value.String()
		if pass == "" {
			pass, err = randString(16)
			if err != nil {
				log.Fatalw("failed to generate password", "error", err)
			}
		}
		remote := cmd.Flag("remote").Value.String()
		user := cmd.Flag("user").Value.String()
		if err := k8s.CreateGitea("", user, pass, remote); err != nil {
			log.Fatalw("failed to create gitea", "error", err)
		}
		if err := k8s.WriteGiteaToFile("infra/gitea/gitea.yaml"); err != nil {
			log.Fatalw("failed to write gitea to file", "error", err)
		}
		if err := r.AddExisting("gitea/gitea.yaml"); err != nil {
			log.Fatalw("failed to add existing gitea", "error", err)
		}
		if err := r.GenerateKustomize("default", "gitea"); err != nil {
			log.Fatalw("failed to generate kustomize", "error", err)
		}

		repoURL := "https://localhost:3000/infra/infra.git"
		if err := r.AddRemote("local", repoURL); err != nil {
			log.Fatalw("failed to add remote", "error", err)
		}
		repoURL = "https://" + remote + "/infra/infra.git"
		if err := r.AddRemote("origin", repoURL); err != nil {
			log.Fatalw("failed to add remote", "error", err)
		}
		if !dryRun {
			failed := true
			go func() {
				forwarder, err := proxy.NewForwarder(ctx, log, cmd.Flag("context").Value.String())
				if err != nil {
					log.Fatalw("failed to create forwarder", "error", err)
				}
				if err := forwarder.ForwardPorts("", "", ""); err != nil {
					log.Fatalw("failed to forward ports", "error", err)
				}
			}()
			for tries := 0; tries < 60; tries++ {
				_, err := http.Get("https://localhost:3000/api/healthz")
				if err == nil {
					break
				}
				time.Sleep(3 * time.Second)
			}
			for tries := 0; tries < 60; tries++ {
				if err := r.PushBasic("local", user, pass); err != nil {
					fmt.Printf("[try %d] push failed: %v\n", tries, err)
				} else {
					failed = false
					break
				}
				time.Sleep(3 * time.Second)
			}
			if failed {
				log.Fatal("failed to push to remote")
			}
		}

		if err := k8s.CreateArgoInit("", user, pass); err != nil {
			log.Fatalw("failed to create argo init", "error", err)
		}
		if err := k8s.WriteArgoToFile("infra/init/init.yaml"); err != nil {
			log.Fatalw("failed to write argo to file", "error", err)
		}
		if err := r.AddExisting("init/init.yaml"); err != nil {
			log.Fatalw("failed to add existing argo", "error", err)
		}
		if err := r.GenerateKustomize("argocd", "init"); err != nil {
			log.Fatalw("failed to generate kustomize", "error", err)
		}
		if !dryRun {
			for tries := 0; tries < 60; tries++ {
				if err := r.PushBasic("local", user, pass); err != nil {
					log.Warnf("push failed: %v", err, zap.Int("try", tries))
				} else {
					break
				}
				time.Sleep(3 * time.Second)
			}
		}

	},
}

func randString(n int) (string, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		ret[i] = letters[num.Int64()]
	}

	return string(ret), nil
}

func init() {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(fmt.Sprintf("crypto/rand is unavailable: Read() failed %#v", err))
	}
	rootCmd.AddCommand(runCmd)
	viper.AutomaticEnv()
	runCmd.Flags().StringP("password", "p", "", "remote password (generated if not set) [env PIVOT_PASSWD]")
	if err := viper.BindPFlag("PIVOT_PASSWD", runCmd.Flags().Lookup("password")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("remote", "r", "git.local.net", "remote repository [env PIVOT_REMOTE]")
	if err := viper.BindPFlag("PIVOT_REMOTE", runCmd.Flags().Lookup("remote")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("user", "u", "pivot", "remote user [env PIVOT_USER]")
	if err := viper.BindPFlag("PIVOT_USER", runCmd.Flags().Lookup("user")); err != nil {
		panic(err)
	}
	runCmd.Flags().BoolP("dry-run", "d", false, "dry run")
	if err := viper.BindPFlag("PIVOT_DRY_RUN", runCmd.Flags().Lookup("dry-run")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("namespace", "n", "", "namespace (context default if not set) [env PIVOT_NAMESPACE]")
	if err := viper.BindPFlag("PIVOT_NAMESPACE", runCmd.Flags().Lookup("namespace")); err != nil {
		panic(err)
	}
}
