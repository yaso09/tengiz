package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func NewStoreWithEnv(dataDir, env string) *Store {
	if env == "" {
		env = "production"
	}
	os.MkdirAll(dataDir, 0755)
	return &Store{dataDir: dataDir, env: env}
}

func (s *Store) envFile(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%s%s", base, s.env, ext)
}

type Store struct {
	mu      sync.Mutex
	dataDir string
	env     string
}

func NewStore(dataDir string) *Store {
	return NewStoreWithEnv(dataDir, "")
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) SaveApp(app types.AppEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	apps[app.Name] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) RemoveApp(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	delete(apps, name)
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) ListApps() ([]types.AppEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
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
	s.readJSON(s.envFile("ports.json"), &ports)

	for p := 9000; p <= 9999; p++ {
		if _, used := ports[p]; !used {
			ports[p] = appName
			if err := s.writeJSON(s.envFile("ports.json"), ports); err != nil {
				return 0, err
			}
			return p, nil
		}
	}
	return 0, nil
}

func (s *Store) CleanupOrphanedPorts() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON(s.envFile("ports.json"), &ports)
	if len(ports) == 0 {
		return 0, nil
	}

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)

	var removed int
	for port, appName := range ports {
		if _, exists := apps[appName]; !exists {
			delete(ports, port)
			removed++
		}
	}

	if removed > 0 {
		if err := s.writeJSON(s.envFile("ports.json"), ports); err != nil {
			return removed, fmt.Errorf("write ports: %w", err)
		}
	}

	return removed, nil
}

func (s *Store) FreePort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON(s.envFile("ports.json"), &ports)
	delete(ports, port)
	return s.writeJSON(s.envFile("ports.json"), ports)
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
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if app.Config.Env == nil {
		app.Config.Env = make(map[string]string)
	}
	app.Config.Env[key] = value
	apps[appName] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) UnsetEnv(appName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	delete(app.Config.Env, key)
	if len(app.Config.Env) == 0 {
		app.Config.Env = nil
	}
	apps[appName] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
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

func (s *Store) GetApp(name string) (*types.AppEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
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
	s.readJSON(s.envFile("apps.json"), &apps)
	apps[app.Name] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) AddDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
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
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) RemoveDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
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
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) ListDomains(appName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]string, len(app.Domains))
	copy(result, app.Domains)
	return result, nil
}

func (s *Store) AddVolume(appName string, vol types.VolumeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, v := range app.Config.Volumes {
		if v.HostPath == vol.HostPath {
			return fmt.Errorf("volume with host path %q already exists for app %q", vol.HostPath, appName)
		}
	}
	app.Config.Volumes = append(app.Config.Volumes, vol)
	apps[appName] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) RemoveVolume(appName, hostPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	found := false
	for i, v := range app.Config.Volumes {
		if v.HostPath == hostPath {
			app.Config.Volumes = append(app.Config.Volumes[:i], app.Config.Volumes[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("volume with host path %q not found for app %q", hostPath, appName)
	}
	apps[appName] = app
	return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) ListVolumes(appName string) ([]types.VolumeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON(s.envFile("apps.json"), &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]types.VolumeConfig, len(app.Config.Volumes))
	copy(result, app.Config.Volumes)
	return result, nil
}

func (s *Store) AddDeployment(appName string, dep types.DeploymentEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON(s.envFile("deployments.json"), &deployments)
	entries := deployments[appName]
	entries = append(entries, dep)
	if len(entries) > 10 {
		entries = entries[len(entries)-10:]
	}
	deployments[appName] = entries
	return s.writeJSON(s.envFile("deployments.json"), deployments)
}

func (s *Store) GetDeployments(appName string) ([]types.DeploymentEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON(s.envFile("deployments.json"), &deployments)
	return deployments[appName], nil
}

func (s *Store) UpdateDeploymentStatus(appName, deploymentID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON(s.envFile("deployments.json"), &deployments)
	entries := deployments[appName]
	found := false
	for i := range entries {
		if entries[i].ID == deploymentID {
			entries[i].Status = status
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("deployment %q not found for app %q", deploymentID, appName)
	}
	deployments[appName] = entries
	return s.writeJSON(s.envFile("deployments.json"), deployments)
}

func (s *Store) GetPreviousDeployment(appName string) (*types.DeploymentEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON(s.envFile("deployments.json"), &deployments)
	entries, ok := deployments[appName]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("no deployment history for app %q", appName)
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Status == string(types.DeployPrevious) {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("no previous deployment found for app %q", appName)
}

func (s *Store) GetDeploymentByID(appName, deploymentID string) (*types.DeploymentEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments := make(map[string][]types.DeploymentEntry)
	s.readJSON(s.envFile("deployments.json"), &deployments)
	entries, ok := deployments[appName]
	if !ok {
		return nil, fmt.Errorf("no deployment history for app %q", appName)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ID == deploymentID {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("deployment %q not found for app %q", deploymentID, appName)
}

func (s *Store) previewsFile() string {
	suffix := ""
	if s.env != "" && s.env != "production" {
		suffix = "-" + s.env
	}
	return filepath.Join(s.dataDir, "previews"+suffix+".json")
}

func (s *Store) loadPreviews() (map[string]types.PreviewEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.previewsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]types.PreviewEntry), nil
		}
		return nil, err
	}
	var previews map[string]types.PreviewEntry
	if err := json.Unmarshal(data, &previews); err != nil {
		return nil, err
	}
	return previews, nil
}

func (s *Store) savePreviews(previews map[string]types.PreviewEntry) error {
	data, err := json.MarshalIndent(previews, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.previewsFile(), data, 0644)
}

func previewKey(appName string, prNumber int) string {
	return fmt.Sprintf("%s/pr-%d", appName, prNumber)
}

func (s *Store) AddPreview(p types.PreviewEntry) error {
	previews, err := s.loadPreviews()
	if err != nil {
		return err
	}
	key := previewKey(p.AppName, p.PRNumber)
	if _, exists := previews[key]; exists {
		return fmt.Errorf("preview %s already exists", key)
	}
	previews[key] = p
	return s.savePreviews(previews)
}

func (s *Store) GetPreview(appName string, prNumber int) (*types.PreviewEntry, error) {
	previews, err := s.loadPreviews()
	if err != nil {
		return nil, err
	}
	key := previewKey(appName, prNumber)
	p, ok := previews[key]
	if !ok {
		return nil, fmt.Errorf("preview %s not found", key)
	}
	return &p, nil
}

func (s *Store) ListPreviews(appName string) ([]types.PreviewEntry, error) {
	previews, err := s.loadPreviews()
	if err != nil {
		return nil, err
	}
	prefix := appName + "/pr-"
	var result []types.PreviewEntry
	for key, p := range previews {
		if strings.HasPrefix(key, prefix) {
			result = append(result, p)
		}
	}
	return result, nil
}

func (s *Store) ListAllPreviews() ([]types.PreviewEntry, error) {
	previews, err := s.loadPreviews()
	if err != nil {
		return nil, err
	}
	var result []types.PreviewEntry
	for _, p := range previews {
		result = append(result, p)
	}
	return result, nil
}

func (s *Store) DeletePreview(appName string, prNumber int) error {
	previews, err := s.loadPreviews()
	if err != nil {
		return err
	}
	key := previewKey(appName, prNumber)
	if _, exists := previews[key]; !exists {
		return fmt.Errorf("preview %s not found", key)
	}
	delete(previews, key)
	return s.savePreviews(previews)
}

func (s *Store) UpdatePreviewDeployment(appName string, prNumber int, imageTag, deploymentID string) error {
	previews, err := s.loadPreviews()
	if err != nil {
		return err
	}
	key := previewKey(appName, prNumber)
	p, ok := previews[key]
	if !ok {
		return fmt.Errorf("preview %s not found", key)
	}
	p.ImageTag = imageTag
	p.DeploymentID = deploymentID
	p.UpdatedAt = time.Now()
	p.Status = string(types.PreviewActive)
	previews[key] = p
	return s.savePreviews(previews)
}

func (s *Store) UpdatePreviewStatus(appName string, prNumber int, status string) error {
	previews, err := s.loadPreviews()
	if err != nil {
		return err
	}
	key := previewKey(appName, prNumber)
	p, ok := previews[key]
	if !ok {
		return fmt.Errorf("preview %s not found", key)
	}
	p.Status = status
	p.UpdatedAt = time.Now()
	previews[key] = p
	return s.savePreviews(previews)
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

func (s *Store) buildLogDir(appName string) string {
	return filepath.Join(s.dataDir, "build-logs", s.env, appName)
}

func (s *Store) SaveBuildLog(appName, deploymentID, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir build-logs: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s.log", deploymentID))
	return os.WriteFile(path, []byte(content), 0644)
}

func (s *Store) GetBuildLog(appName, deploymentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.buildLogDir(appName), fmt.Sprintf("%s.log", deploymentID))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read build log: %w", err)
	}
	return string(data), nil
}

func (s *Store) ListBuildLogs(appName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list build logs: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".log"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

func (s *Store) PruneBuildLogs(appName string, keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("prune list: %w", err)
	}

	type logFile struct {
		name string
		info os.FileInfo
	}
	var files []logFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, logFile{name: e.Name(), info: info})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].info.ModTime().Before(files[j].info.ModTime())
	})

	if len(files) <= keep {
		return nil
	}

	for _, f := range files[:len(files)-keep] {
		os.Remove(filepath.Join(dir, f.name))
	}
	return nil
}
