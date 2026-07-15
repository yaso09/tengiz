package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/yaso09/tengiz/internal/types"
)

type Store struct {
	mu      sync.Mutex
	dataDir string
}

func NewStore(dataDir string) *Store {
	os.MkdirAll(dataDir, 0755)
	return &Store{dataDir: dataDir}
}

func (s *Store) SaveApp(app types.AppEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	apps[app.Name] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveApp(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	delete(apps, name)
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListApps() ([]types.AppEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	result := make([]types.AppEntry, 0, len(apps))
	for _, v := range apps {
		result = append(result, v)
	}
	return result, nil
}

func (s *Store) AllocatePort(appName string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON("ports.json", &ports)

	for p := 9000; p <= 9999; p++ {
		if _, used := ports[p]; !used {
			ports[p] = appName
			if err := s.writeJSON("ports.json", ports); err != nil {
				return 0, err
			}
			return p, nil
		}
	}
	return 0, nil
}

func (s *Store) FreePort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON("ports.json", &ports)
	delete(ports, port)
	return s.writeJSON("ports.json", ports)
}

func (s *Store) GetEnv(appName, key string) (string, bool, error) {
	app, err := s.GetApp(appName)
	if err != nil {
		return "", false, err
	}
	val, ok := app.Config.Env[key]
	return val, ok, nil
}

func (s *Store) SetEnv(appName, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if app.Config.Env == nil {
		app.Config.Env = make(map[string]string)
	}
	app.Config.Env[key] = value
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) UnsetEnv(appName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	delete(app.Config.Env, key)
	if len(app.Config.Env) == 0 {
		app.Config.Env = nil
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListEnv(appName string) (map[string]string, error) {
	app, err := s.GetApp(appName)
	if err != nil {
		return nil, err
	}
	if app.Config.Env == nil {
		return map[string]string{}, nil
	}
	result := make(map[string]string, len(app.Config.Env))
	for k, v := range app.Config.Env {
		result[k] = v
	}
	return result, nil
}

func (s *Store) AddVolume(appName string, mount types.VolumeMount) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, v := range app.Volumes {
		if v.HostPath == mount.HostPath && v.ContainerPath == mount.ContainerPath {
			return fmt.Errorf("volume %s:%s already exists for app %q", mount.HostPath, mount.ContainerPath, appName)
		}
	}
	app.Volumes = append(app.Volumes, mount)
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveVolume(appName, hostPath, containerPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	found := false
	for i, v := range app.Volumes {
		if v.HostPath == hostPath && v.ContainerPath == containerPath {
			app.Volumes = append(app.Volumes[:i], app.Volumes[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("volume %s:%s not found for app %q", hostPath, containerPath, appName)
	}
	if len(app.Volumes) == 0 {
		app.Volumes = nil
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListVolumes(appName string) ([]types.VolumeMount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]types.VolumeMount, len(app.Volumes))
	copy(result, app.Volumes)
	return result, nil
}

func (s *Store) GetApp(name string) (*types.AppEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[name]
	if !ok {
		return nil, fmt.Errorf("app %q not found", name)
	}
	return &app, nil
}

func (s *Store) UpdateApp(app types.AppEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	apps[app.Name] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) AddDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, d := range app.Domains {
		if d == domain {
			return fmt.Errorf("domain %q already added to app %q", domain, appName)
		}
	}
	app.Domains = append(app.Domains, domain)
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	found := false
	for i, d := range app.Domains {
		if d == domain {
			app.Domains = append(app.Domains[:i], app.Domains[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("domain %q not found for app %q", domain, appName)
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListDomains(appName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]string, len(app.Domains))
	copy(result, app.Domains)
	return result, nil
}

func (s *Store) AddDeployment(appName string, dep types.DeploymentEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON("deployments.json", &deployments)
	entries := deployments[appName]
	entries = append(entries, dep)
	if len(entries) > 10 {
		entries = entries[len(entries)-10:]
	}
	deployments[appName] = entries
	return s.writeJSON("deployments.json", deployments)
}

func (s *Store) GetDeployments(appName string) ([]types.DeploymentEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON("deployments.json", &deployments)
	return deployments[appName], nil
}

func (s *Store) readJSON(name string, v interface{}) {
	data, err := os.ReadFile(filepath.Join(s.dataDir, name))
	if err == nil {
		json.Unmarshal(data, v)
	}
}

func (s *Store) writeJSON(name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, name), data, 0644)
}
