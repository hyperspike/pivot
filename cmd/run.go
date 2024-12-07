package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/url"

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
		k8s, err := spool.NewK8s()
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
		url, err := url.Parse(remote)
		if err != nil {
			panic(err)
		}
		user := cmd.Flag("user").Value.String()
		if err := k8s.CreateGitea("", user, pass, url.Hostname()); err != nil {
			panic(err)
		}
		if err := r.AddRemote(remote); err != nil {
			panic(err)
		}
		if err := r.PushBasic(user, pass); err != nil {
			panic(err)
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
	runCmd.Flags().StringP("password", "p", "", "remote password [env PIVOT_PASSWD]")
	if err := viper.BindPFlag("PIVOT_PASSWD", runCmd.Flags().Lookup("password")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("remote", "r", "", "remote repository [env PIVOT_REMOTE]")
	if err := viper.BindPFlag("PIVOT_REMOTE", runCmd.Flags().Lookup("remote")); err != nil {
		panic(err)
	}
	runCmd.Flags().StringP("user", "u", "pivot", "remote user [env PIVOT_USER]")
	if err := viper.BindPFlag("PIVOT_USER", runCmd.Flags().Lookup("user")); err != nil {
		panic(err)
	}
}
