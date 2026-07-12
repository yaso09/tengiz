package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"github.com/yasir/tengiz/internal/types"
)

const defaultIdleTimeout = 5 * time.Minute

func Load(path string) (*types.AppConfig, error) {
	configPath := filepath.Join(path, ".tengiz.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf(".tengiz.yaml not found in %s", path)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	v.SetDefault("serverless.enabled", true)
	v.SetDefault("serverless.idle_timeout", defaultIdleTimeout)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config read: %w", err)
	}

	var cfg types.AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config unmarshal: %w", err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("config: 'name' field is required")
	}

	return &cfg, nil
}

func FindProjectRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, ".tengiz.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(".tengiz.yaml not found from %s", path)
		}
		dir = parent
	}
}
