package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/yaso09/tengiz/internal/encrypt"
)

type secretsFile struct {
	Apps map[string]map[string]string `json:"apps"`
}

type Manager struct {
	mu      sync.Mutex
	dataDir string
	env     string
	key     []byte
}

func NewManager(dataDir, env string) (*Manager, error) {
	if env == "" {
		env = "production"
	}
	os.MkdirAll(dataDir, 0755)

	keyPath := filepath.Join(dataDir, ".key")
	key, err := encrypt.LoadKey(keyPath)
	if err != nil {
		key, err = encrypt.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := encrypt.SaveKey(keyPath, key); err != nil {
			return nil, fmt.Errorf("save key: %w", err)
		}
	}

	return &Manager{
		dataDir: dataDir,
		env:     env,
		key:     key,
	}, nil
}

func (m *Manager) secretsPath() string {
	name := fmt.Sprintf("secrets-%s.json", m.env)
	return filepath.Join(m.dataDir, name)
}

func (m *Manager) load() (*secretsFile, error) {
	sf := &secretsFile{Apps: make(map[string]map[string]string)}
	data, err := os.ReadFile(m.secretsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return sf, nil
		}
		return nil, fmt.Errorf("read secrets: %w", err)
	}

	decrypted, err := encrypt.Decrypt(data, m.key)
	if err != nil {
		return nil, fmt.Errorf("decrypt secrets: %w", err)
	}

	if err := json.Unmarshal(decrypted, sf); err != nil {
		return nil, fmt.Errorf("unmarshal secrets: %w", err)
	}
	if sf.Apps == nil {
		sf.Apps = make(map[string]map[string]string)
	}
	return sf, nil
}

func (m *Manager) save(sf *secretsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	encrypted, err := encrypt.Encrypt(data, m.key)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}

	return os.WriteFile(m.secretsPath(), encrypted, 0644)
}

func (m *Manager) Set(appName, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return err
	}

	if sf.Apps[appName] == nil {
		sf.Apps[appName] = make(map[string]string)
	}
	sf.Apps[appName][key] = value

	return m.save(sf)
}

func (m *Manager) Get(appName, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return "", false, err
	}

	appSecrets, ok := sf.Apps[appName]
	if !ok {
		return "", false, nil
	}
	val, ok := appSecrets[key]
	return val, ok, nil
}

func (m *Manager) Unset(appName, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return err
	}

	if sf.Apps[appName] != nil {
		delete(sf.Apps[appName], key)
		if len(sf.Apps[appName]) == 0 {
			delete(sf.Apps, appName)
		}
	}

	return m.save(sf)
}

func (m *Manager) List(appName string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return nil, err
	}

	appSecrets := sf.Apps[appName]
	if appSecrets == nil {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(appSecrets))
	for k, v := range appSecrets {
		result[k] = v
	}
	return result, nil
}

func (m *Manager) GetAllForApp(appName string) (map[string]string, error) {
	return m.List(appName)
}
