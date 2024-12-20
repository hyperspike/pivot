package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// Version is the version of the application
	Version string
	Commit  string
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print the version of the application",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Go version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
