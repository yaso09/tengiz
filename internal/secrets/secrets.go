package secrets

import (
	"fmt"
	"sync"
)

type secretsFile struct {
	Apps map[string]map[string]string `json:"apps"`
}

type Manager struct {
	mu       sync.Mutex
	dataDir  string
	env      string
	provider Provider
}

func NewManager(dataDir, env string) (*Manager, error) {
	p, err := NewLocalProvider(dataDir, env)
	if err != nil {
		return nil, err
	}
	return &Manager{
		dataDir:  dataDir,
		env:      env,
		provider: p,
	}, nil
}

func NewManagerWithProvider(p Provider) *Manager {
	return &Manager{
		env:      "production",
		provider: p,
	}
}

func (m *Manager) Set(appName, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider.Set(appName, key, value)
}

func (m *Manager) Get(appName, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider.Get(appName, key)
}

func (m *Manager) Unset(appName, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider.Unset(appName, key)
}

func (m *Manager) List(appName string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider.List(appName)
}

func (m *Manager) GetAllForApp(appName string) (map[string]string, error) {
	return m.List(appName)
}

func (m *Manager) Provider() Provider {
	return m.provider
}

func NewManagerFromConfig(dataDir, env, provider, vaultAddr, vaultToken, dopplerToken, dopplerProject, dopplerConfig string) (*Manager, error) {
	switch provider {
	case "", "local":
		return NewManager(dataDir, env)
	case "vault":
		p, err := NewVaultProvider(VaultConfig{
			Address: vaultAddr,
			Token:   vaultToken,
		})
		if err != nil {
			return nil, err
		}
		return NewManagerWithProvider(p), nil
	case "doppler":
		p, err := NewDopplerProvider(DopplerConfig{
			Token:   dopplerToken,
			Project: dopplerProject,
			Config:  dopplerConfig,
		})
		if err != nil {
			return nil, err
		}
		return NewManagerWithProvider(p), nil
	default:
		return nil, fmt.Errorf("unknown secrets provider: %q (supported: local, vault, doppler)", provider)
	}
}
