package secrets

import (
	"fmt"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

type VaultConfig struct {
	Address string
	Token   string
	Path    string
}

type VaultProvider struct {
	client *vault.Client
	path   string
}

func NewVaultProvider(cfg VaultConfig) (*VaultProvider, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("vault address is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("vault token is required")
	}
	client, err := vault.NewClient(&vault.Config{
		Address: cfg.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}
	client.SetToken(cfg.Token)
	path := cfg.Path
	if path == "" {
		path = "secret"
	}
	return &VaultProvider{client: client, path: strings.TrimSuffix(path, "/")}, nil
}

func (p *VaultProvider) Name() string { return "vault" }

func (p *VaultProvider) secretPath(appName string) string {
	return fmt.Sprintf("%s/data/%s", p.path, appName)
}

func (p *VaultProvider) metadataPath(appName string) string {
	return fmt.Sprintf("%s/metadata/%s", p.path, appName)
}

func (p *VaultProvider) Set(appName, key, value string) error {
	sp := p.secretPath(appName)
	secret, err := p.client.Logical().Read(sp)
	if err != nil {
		return fmt.Errorf("vault read: %w", err)
	}
	data := make(map[string]interface{})
	if secret != nil {
		if d, ok := secret.Data["data"].(map[string]interface{}); ok {
			for k, v := range d {
				if s, ok := v.(string); ok {
					data[k] = s
				}
			}
		}
	}
	data[key] = value
	_, err = p.client.Logical().Write(sp, map[string]interface{}{
		"data": data,
	})
	if err != nil {
		return fmt.Errorf("vault write: %w", err)
	}
	return nil
}

func (p *VaultProvider) Get(appName, key string) (string, bool, error) {
	secret, err := p.client.Logical().Read(p.secretPath(appName))
	if err != nil {
		return "", false, fmt.Errorf("vault read: %w", err)
	}
	if secret == nil {
		return "", false, nil
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", false, nil
	}
	val, ok := data[key].(string)
	if !ok {
		return "", false, nil
	}
	return val, true, nil
}

func (p *VaultProvider) Unset(appName, key string) error {
	sp := p.secretPath(appName)
	secret, err := p.client.Logical().Read(sp)
	if err != nil {
		return fmt.Errorf("vault read: %w", err)
	}
	if secret == nil {
		return nil
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok || data == nil {
		return nil
	}
	delete(data, key)
	if len(data) == 0 {
		_, err = p.client.Logical().Delete(p.metadataPath(appName))
		return err
	}
	_, err = p.client.Logical().Write(sp, map[string]interface{}{
		"data": data,
	})
	return err
}

func (p *VaultProvider) List(appName string) (map[string]string, error) {
	secret, err := p.client.Logical().Read(p.secretPath(appName))
	if err != nil {
		return nil, fmt.Errorf("vault read: %w", err)
	}
	if secret == nil {
		return map[string]string{}, nil
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok || data == nil {
		return map[string]string{}, nil
	}
	result := make(map[string]string, len(data))
	for k, v := range data {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result, nil
}
