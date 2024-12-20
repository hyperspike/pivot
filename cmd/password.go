package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"hyperspike.io/pivot/internal/kubernetes"
)

var passwordCmd = &cobra.Command{
	Use:   "password",
	Short: "fetch the generated pivot password",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.TODO()
		kube, err := kubernetes.NewK8s(ctx, getLogger(cmd), cmd.Flag("context").Value.String(), false)
		if err != nil {
			panic(err)
		}
		pass, err := kube.GetPivotPassword()
		if err != nil {
			panic(err)
		}
		fmt.Println(pass)
	},
}

func init() {
	rootCmd.AddCommand(passwordCmd)
}
