package main

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

func init() {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	var err error
	Logger, err = config.Build()
	if err != nil {
		panic(err)
	}
}

func getLogger(cmd *cobra.Command) *zap.SugaredLogger {
	var err error
	if cmd.Flag("format").Value.String() == "json" {
		Logger, err = zap.NewProduction()
		if err != nil {
			panic(err)
		}
	}
	defer Logger.Sync()
	return Logger.Sugar()
}
