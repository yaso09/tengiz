package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yaso09/tengiz/internal/encrypt"
)

type LocalProvider struct {
	dataDir string
	env     string
	key     []byte
}

func NewLocalProvider(dataDir, env string) (*LocalProvider, error) {
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

	return &LocalProvider{
		dataDir: dataDir,
		env:     env,
		key:     key,
	}, nil
}

func NewLocalProviderWithKey(dataDir, env string, key []byte) *LocalProvider {
	if env == "" {
		env = "production"
	}
	return &LocalProvider{dataDir: dataDir, env: env, key: key}
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) secretsPath() string {
	return filepath.Join(p.dataDir, fmt.Sprintf("secrets-%s.json", p.env))
}

func (p *LocalProvider) load() (*secretsFile, error) {
	sf := &secretsFile{Apps: make(map[string]map[string]string)}
	data, err := os.ReadFile(p.secretsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return sf, nil
		}
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	decrypted, err := encrypt.Decrypt(data, p.key)
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

func (p *LocalProvider) save(sf *secretsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	encrypted, err := encrypt.Encrypt(data, p.key)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}
	return os.WriteFile(p.secretsPath(), encrypted, 0644)
}

func (p *LocalProvider) Set(appName, key, value string) error {
	sf, err := p.load()
	if err != nil {
		return err
	}
	if sf.Apps[appName] == nil {
		sf.Apps[appName] = make(map[string]string)
	}
	sf.Apps[appName][key] = value
	return p.save(sf)
}

func (p *LocalProvider) Get(appName, key string) (string, bool, error) {
	sf, err := p.load()
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

func (p *LocalProvider) Unset(appName, key string) error {
	sf, err := p.load()
	if err != nil {
		return err
	}
	if sf.Apps[appName] != nil {
		delete(sf.Apps[appName], key)
		if len(sf.Apps[appName]) == 0 {
			delete(sf.Apps, appName)
		}
	}
	return p.save(sf)
}

func (p *LocalProvider) List(appName string) (map[string]string, error) {
	sf, err := p.load()
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
