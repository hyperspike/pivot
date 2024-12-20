package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"go.uber.org/zap"
)

var rootCmd = &cobra.Command{
	Use:   "pivot",
	Short: "Pivot is a tool for pivoting from bootstrap to GitOps",
}

func main() {
	logger := zap.NewExample().Sugar()
	defer logger.Sync()
	rootCmd.PersistentFlags().StringP("format", "f", "text", "output format")
	rootCmd.PersistentFlags().StringP("context", "c", "", "use an explicit Kubernetes context [env PIVOT_CONTEXT]")
	if err := viper.BindPFlag("PIVOT_CONTEXT", rootCmd.PersistentFlags().Lookup("context")); err != nil {
		logger.Fatalln("failed to bind flag", err)
	}
	if err := rootCmd.Execute(); err != nil {
		logger.Fatalln("failed to execute command", err)
	}
}
