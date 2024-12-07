package main

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pivot",
	Short: "Pivot is a tool for pivoting from bootstrap to GitOps",
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
