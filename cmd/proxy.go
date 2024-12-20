package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"hyperspike.io/pivot/internal/proxy"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "proxy a service",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.TODO()
		forwarder, err := proxy.NewForwarder(ctx, getLogger(cmd), cmd.Flag("context").Value.String())
		if err != nil {
			panic(err)
		}
		if err := forwarder.ForwardPorts(cmd.Flag("name").Value.String(), cmd.Flag("namespace").Value.String(), cmd.Flag("port").Value.String()); err != nil {
			panic(err)
		}
	},
}

func init() {
	viper.AutomaticEnv()
	proxyCmd.Flags().String("name", "", "name of the pod to proxy")
	if err := viper.BindPFlag("POD_NAME", proxyCmd.Flags().Lookup("name")); err != nil {
		panic(err)
	}
	proxyCmd.Flags().String("namespace", "", "namespace of the pod to proxy")
	if err := viper.BindPFlag("POD_NAMESPACE", proxyCmd.Flags().Lookup("namespace")); err != nil {
		panic(err)
	}
	proxyCmd.Flags().String("port", "", "port to forward")
	if err := viper.BindPFlag("POD_PORT", proxyCmd.Flags().Lookup("port")); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(proxyCmd)
}
