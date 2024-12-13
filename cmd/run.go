package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"hyperspike.io/pivot/internal/spool"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "start pivoting",
	Run: func(cmd *cobra.Command, args []string) {
		r, err := spool.CreateRepo("infra")
		if err != nil {
			panic(err)
		}
		dryRun := cmd.Flag("dry-run").Value.String() == "true"
		k8s, err := spool.NewK8s(dryRun)
		if err != nil {
			panic(err)
		}
		if err := k8s.ApplyKustomize("infra/cert-manager"); err != nil {
			panic(err)
		}
		if err := k8s.ApplyKustomize("infra/argocd"); err != nil {
			panic(err)
		}
		if err := k8s.ApplyKustomize("infra/postgres-operator"); err != nil {
			panic(err)
		}
		if err := k8s.ApplyKustomize("infra/valkey-operator"); err != nil {
			panic(err)
		}
		if err := k8s.ApplyKustomize("infra/gitea-operator"); err != nil {
			panic(err)
		}
		pass := cmd.Flag("password").Value.String()
		if pass == "" {
			pass, err = randString(16)
			if err != nil {
				panic(err)
			}
		}
		remote := cmd.Flag("remote").Value.String()
		user := cmd.Flag("user").Value.String()
		if err := k8s.CreateGitea("", user, pass, remote); err != nil {
			panic(err)
		}
		if err := k8s.WriteGiteaToFile("infra/gitea/gitea.yaml"); err != nil {
			panic(err)
		}
		if err := r.AddExisting("gitea/gitea.yaml"); err != nil {
			panic(err)
		}
		if err := r.GenerateKustomize("default", "gitea"); err != nil {
			panic(err)
		}

		for tries := 0; tries < 60; tries++ {
			_, err := http.Get("https://" + remote + "/api/healthz")
			if err == nil {
				break
			}
			time.Sleep(3 * time.Second)
		}
		repoURL := "https://" + remote + "/infra/infra.git"
		if err := r.AddRemote(repoURL); err != nil {
			panic(err)
		}
		failed := true
		if !dryRun {
			for tries := 0; tries < 60; tries++ {
				if err := r.PushBasic(user, pass); err != nil {
					fmt.Printf("[try %d] push failed: %v\n", tries, err)
				} else {
					failed = false
					break
				}
				time.Sleep(3 * time.Second)
			}
		}
		if failed {
			panic("failed to push to remote")
			return
		}

		if err := k8s.CreateArgoInit("", user, pass); err != nil {
			panic(err)
		}
		if err := k8s.WriteArgoToFile("infra/init/init.yaml"); err != nil {
			panic(err)
		}
		if err := r.AddExisting("init/init.yaml"); err != nil {
			panic(err)
		}
		if err := r.GenerateKustomize("argocd", "init"); err != nil {
			panic(err)
		}
		if !dryRun {
			for tries := 0; tries < 60; tries++ {
				if err := r.PushBasic(user, pass); err != nil {
					fmt.Printf("[try %d] push failed: %v\n", tries, err)
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
	runCmd.Flags().StringP("namespace", "n", "pivot", "namespace [env PIVOT_NAMESPACE]")
	if err := viper.BindPFlag("PIVOT_NAMESPACE", runCmd.Flags().Lookup("namespace")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("context", "c", "", "context [env PIVOT_CONTEXT]")
	if err := viper.BindPFlag("PIVOT_CONTEXT", runCmd.Flags().Lookup("context")); err != nil {
		panic(err)
	}
}
