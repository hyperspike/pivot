package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hyperspike.io/pivot/internal/spool"
)

var passwordCmd = &cobra.Command{
	Use:   "password",
	Short: "fetch the generated pivot password",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.TODO()
		kube, err := spool.NewK8s(ctx, cmd.Flag("context").Value.String(), false)
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
	viper.AutomaticEnv()
	passwordCmd.Flags().String("context", "", "kubernetes context, defaults to current context")
	if err := viper.BindPFlag("PIVOT_CONTEXT", passwordCmd.Flags().Lookup("context")); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(passwordCmd)
}
