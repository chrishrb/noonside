package config

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Dir is the directory of the loaded config files; relative path settings
// (media_dir, migrations_dir, …) are relative to it.
var Dir string

func ReadConfig() {
	viper.AddConfigPath("config")
	viper.AddConfigPath("backend/config") // run from the repository root
	viper.SetConfigName("default_config")
	viper.AutomaticEnv()
	err := viper.ReadInConfig()
	if err != nil {
		slog.Error("failed to read config", "error", err)
		os.Exit(1)
	}
	if abs, err := filepath.Abs(filepath.Dir(viper.ConfigFileUsed())); err == nil {
		Dir = abs
	}

	environment := viper.GetString("ENVIRONMENT")
	viper.SetConfigName(environment + "_config")

	if err := viper.MergeInConfig(); err != nil {
		slog.Error("Error reading env specfic config file", "env", environment, "error", err)
	}
	slog.Info("config loaded and merged", "env", environment)
}
