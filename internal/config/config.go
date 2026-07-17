package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"github.com/yaso09/tengiz/internal/types"
)

func LoadWithEnv(path, env string) (*types.AppConfig, error) {
	if env == "" {
		env = "production"
	}

	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	envPath := filepath.Join(path, fmt.Sprintf(".tengiz.%s.yaml", env))
	if _, statErr := os.Stat(envPath); statErr != nil {
		cfg.Environment = env
		return cfg, nil
	}

	v := viper.New()
	v.SetConfigFile(envPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("env config read: %w", err)
	}

	// Merge env vars (viper lowercases keys, so use GetStringMapString)
	if envVars := v.GetStringMapString("env"); len(envVars) > 0 {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string)
		}
		for k, v := range envVars {
			cfg.Env[k] = v
		}
	}

	// Merge other scalar fields using raw key access
	allSettings := v.AllSettings()
	for key, val := range allSettings {
		switch key {
		case "port":
			if port, ok := val.(int); ok && port != 0 {
				cfg.Port = port
			}
		case "name":
			if name, ok := val.(string); ok && name != "" {
				cfg.Name = name
			}
		}
	}

	cfg.Environment = env
	return cfg, nil
}

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
