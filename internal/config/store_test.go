package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func TestGetAppNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_, err := s.GetApp("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestSaveAndGetApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	app := types.AppEntry{
		Name:             "myapp",
		ImageTag:         "tengiz-apps/myapp:latest",
		Port:             9001,
		DeploymentSuffix: "v1",
	}
	if err := s.SaveApp(app); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetApp("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 9001 {
		t.Errorf("port = %d, want 9001", got.Port)
	}
	if got.DeploymentSuffix != "v1" {
		t.Errorf("DeploymentSuffix = %q, want v1", got.DeploymentSuffix)
	}
}

func TestStoreSetGetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Save an app first
	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	// Set env
	if err := s.SetEnv("testapp", "DATABASE_URL", "postgres://localhost/db"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}

	// Get env
	val, ok, err := s.GetEnv("testapp", "DATABASE_URL")
	if err != nil {
		t.Fatalf("GetEnv: %v", err)
	}
	if !ok {
		t.Fatal("expected env to exist")
	}
	if val != "postgres://localhost/db" {
		t.Fatalf("expected 'postgres://localhost/db', got %q", val)
	}
}

func TestStoreUnsetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"MY_KEY": "myval"},
	}})

	// Unset
	if err := s.UnsetEnv("testapp", "MY_KEY"); err != nil {
		t.Fatalf("UnsetEnv: %v", err)
	}

	// Verify gone
	_, ok, err := s.GetEnv("testapp", "MY_KEY")
	if err != nil {
		t.Fatalf("GetEnv after unset: %v", err)
	}
	if ok {
		t.Fatal("expected env to be unset")
	}
}

func TestStoreListEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"A": "1", "B": "2"},
	}})

	env, err := s.ListEnv("testapp")
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(env))
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Fatalf("unexpected env map: %v", env)
	}
}

func TestStoreAddDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddDomain("testapp", "example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := s.AddDomain("testapp", "api.example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(app.Domains))
	}
	if app.Domains[0] != "example.com" {
		t.Errorf("domains[0] = %q, want example.com", app.Domains[0])
	}
}

func TestStoreAddDomainDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	s.AddDomain("testapp", "example.com")
	err := s.AddDomain("testapp", "example.com")
	if err == nil {
		t.Fatal("expected error for duplicate domain")
	}
}

func TestStoreRemoveDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:    "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config:  types.AppConfig{Name: "testapp"},
	})

	if err := s.RemoveDomain("testapp", "example.com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(app.Domains))
	}
	if app.Domains[0] != "api.example.com" {
		t.Errorf("domains[0] = %q, want api.example.com", app.Domains[0])
	}
}

func TestStoreRemoveDomainNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveDomain("testapp", "nonexistent.com")
	if err == nil {
		t.Fatal("expected error for non-existent domain")
	}
}

func TestStoreListDomains(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:    "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config:  types.AppConfig{Name: "testapp"},
	})

	domains, err := s.ListDomains("testapp")
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
}

func TestStoreListDomainsNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.ListDomains("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestVolumeCRUD(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	app := types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
		},
	}
	if err := s.SaveApp(app); err != nil {
		t.Fatal(err)
	}

	vol := types.VolumeConfig{HostPath: "/host/data", ContainerPath: "/container/data"}

	if err := s.AddVolume("testapp", vol); err != nil {
		t.Fatal(err)
	}

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/data" {
		t.Fatalf("expected /host/data, got %s", vols[0].HostPath)
	}

	roVol := types.VolumeConfig{HostPath: "/host/config", ContainerPath: "/container/config", ReadOnly: true}
	if err := s.AddVolume("testapp", roVol); err != nil {
		t.Fatal(err)
	}

	vols, err = s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}

	if err := s.RemoveVolume("testapp", "/host/data"); err != nil {
		t.Fatal(err)
	}

	vols, err = s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume after removal, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/config" {
		t.Fatalf("expected remaining volume to be /host/config, got %s", vols[0].HostPath)
	}
}

func TestVolumeCRUDNonexistentApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	err := s.AddVolume("noexist", types.VolumeConfig{})
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestAddDeploymentHistory(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	dep := types.DeploymentEntry{
		ID:       "v1",
		ImageTag: "tengiz-apps/myapp:latest",
		Port:     9001,
		Status:   string(types.DeployActive),
	}
	if err := s.AddDeployment("myapp", dep); err != nil {
		t.Fatal(err)
	}
	deps, err := s.GetDeployments("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("got %d deployments, want 1", len(deps))
	}
	if deps[0].ID != "v1" {
		t.Errorf("deployment ID = %q, want v1", deps[0].ID)
	}
}

func TestGetPreviousDeployment(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.GetPreviousDeployment("testapp")
	if err == nil {
		t.Fatal("expected error for no history")
	}

	s.AddDeployment("testapp", types.DeploymentEntry{
		ID: "v1", ImageTag: "img:v1", Port: 9001,
		CreatedAt: time.Now(), Status: string(types.DeployPrevious),
	})
	s.AddDeployment("testapp", types.DeploymentEntry{
		ID: "v2", ImageTag: "img:v2", Port: 9002,
		CreatedAt: time.Now(), Status: string(types.DeployActive),
	})

	dep, err := s.GetPreviousDeployment("testapp")
	if err != nil {
		t.Fatalf("GetPreviousDeployment() error = %v", err)
	}
	if dep.ID != "v1" {
		t.Errorf("expected v1, got %s", dep.ID)
	}
}

func TestUpdateDeploymentStatus(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.AddDeployment("testapp", types.DeploymentEntry{
		ID: "v1", ImageTag: "img:v1", Port: 9001, Status: string(types.DeployActive),
	})

	if err := s.UpdateDeploymentStatus("testapp", "v1", string(types.DeployRolled)); err != nil {
		t.Fatalf("UpdateDeploymentStatus() error = %v", err)
	}

	deps, _ := s.GetDeployments("testapp")
	if deps[0].Status != string(types.DeployRolled) {
		t.Errorf("status = %q, want %q", deps[0].Status, types.DeployRolled)
	}

	err := s.UpdateDeploymentStatus("testapp", "v999", "rolled")
	if err == nil {
		t.Fatal("expected error for non-existent deployment")
	}
}

func TestGetDeploymentByID(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.AddDeployment("testapp", types.DeploymentEntry{ID: "v1", ImageTag: "img:v1", Port: 9001, Status: string(types.DeployPrevious)})
	dep, err := s.GetDeploymentByID("testapp", "v1")
	if err != nil {
		t.Fatalf("GetDeploymentByID() error = %v", err)
	}
	if dep.ID != "v1" {
		t.Errorf("got %s, want v1", dep.ID)
	}
	_, err = s.GetDeploymentByID("testapp", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestSaveAndGetBuildLog(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	if err := s.SaveBuildLog("testapp", "v123", "build output here"); err != nil {
		t.Fatalf("SaveBuildLog() error = %v", err)
	}

	logs, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatalf("ListBuildLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 build log, got %d", len(logs))
	}
	if logs[0] != "v123" {
		t.Errorf("expected deployment ID 'v123', got %q", logs[0])
	}
}

func TestGetBuildLogContent(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveBuildLog("testapp", "v1", "line1\nline2\nline3\n")

	content, err := s.GetBuildLog("testapp", "v1")
	if err != nil {
		t.Fatalf("GetBuildLog() error = %v", err)
	}
	if !strings.Contains(content, "line1") {
		t.Errorf("expected content to contain 'line1', got %q", content)
	}
}

func TestGetBuildLogNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.GetBuildLog("testapp", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent build log")
	}
}

func TestStoreEnvironmentScoping(t *testing.T) {
	dir := t.TempDir()

	prodStore := NewStoreWithEnv(dir, "production")
	app := types.AppEntry{Name: "myapp", Port: 9000}
	if err := prodStore.SaveApp(app); err != nil {
		t.Fatalf("SaveApp (prod): %v", err)
	}

	stageStore := NewStoreWithEnv(dir, "staging")
	stageApp := types.AppEntry{Name: "myapp", Port: 9001}
	if err := stageStore.SaveApp(stageApp); err != nil {
		t.Fatalf("SaveApp (staging): %v", err)
	}

	prodApp, err := prodStore.GetApp("myapp")
	if err != nil {
		t.Fatalf("GetApp (prod): %v", err)
	}
	if prodApp.Port != 9000 {
		t.Errorf("expected prod port 9000, got %d", prodApp.Port)
	}

	stgApp, err := stageStore.GetApp("myapp")
	if err != nil {
		t.Fatalf("GetApp (staging): %v", err)
	}
	if stgApp.Port != 9001 {
		t.Errorf("expected staging port 9001, got %d", stgApp.Port)
	}

	prodFile := filepath.Join(dir, "apps-production.json")
	stageFile := filepath.Join(dir, "apps-staging.json")
	if _, err := os.Stat(prodFile); os.IsNotExist(err) {
		t.Errorf("production apps file not found: %s", prodFile)
	}
	if _, err := os.Stat(stageFile); os.IsNotExist(err) {
		t.Errorf("staging apps file not found: %s", stageFile)
	}
}

func TestStoreDefaultEnv(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithEnv(dir, "")
	if err := store.SaveApp(types.AppEntry{Name: "myapp", Port: 9000}); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}
	prodFile := filepath.Join(dir, "apps-production.json")
	if _, err := os.Stat(prodFile); os.IsNotExist(err) {
		t.Errorf("expected production apps file: %s", prodFile)
	}
}

func TestPreviewStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	s := NewStoreWithEnv(dir, "production")

	preview := types.PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		Branch:        "feature/awesome",
		RepoURL:       "https://github.com/user/myapp.git",
		ImageTag:      "tengiz-apps/myapp:pr-42-1712345678",
		Port:          9100,
		ContainerName: "tengiz-myapp-pr-42",
		DeploymentID:  "1712345678",
		Status:        string(types.PreviewActive),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := s.AddPreview(preview); err != nil {
		t.Fatalf("AddPreview: %v", err)
	}

	list, err := s.ListPreviews("myapp")
	if err != nil {
		t.Fatalf("ListPreviews: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListPreviews returned %d, want 1", len(list))
	}

	got, err := s.GetPreview("myapp", 42)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if got.PRNumber != 42 || got.AppName != "myapp" {
		t.Errorf("unexpected preview: %+v", got)
	}

	if err := s.UpdatePreviewDeployment("myapp", 42, "tengiz-apps/myapp:pr-42-1712349999", "1712349999"); err != nil {
		t.Fatalf("UpdatePreviewDeployment: %v", err)
	}
	got, _ = s.GetPreview("myapp", 42)
	if got.DeploymentID != "1712349999" {
		t.Errorf("DeploymentID = %q, want 1712349999", got.DeploymentID)
	}

	if err := s.DeletePreview("myapp", 42); err != nil {
		t.Fatalf("DeletePreview: %v", err)
	}
	_, err = s.GetPreview("myapp", 42)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestPreviewFullLifecycleNaming(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	appName := "myapp"
	prNumber := 42

	key := previewKey(appName, prNumber)
	expectedKey := "myapp/pr-42"
	if key != expectedKey {
		t.Errorf("previewKey = %q, want %q", key, expectedKey)
	}

	containerName := fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber)
	if containerName != "tengiz-myapp-pr-42" {
		t.Errorf("container name = %q", containerName)
	}

	preview := types.PreviewEntry{
		AppName:       appName,
		PRNumber:      prNumber,
		Branch:        "feature/login",
		ContainerName: containerName,
		Port:          9001,
		Status:        string(types.PreviewActive),
	}
	s.AddPreview(preview)

	got, err := s.GetPreview(appName, prNumber)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if got.ContainerName != "tengiz-myapp-pr-42" {
		t.Errorf("container name = %q", got.ContainerName)
	}

	previews, err := s.ListPreviews(appName)
	if err != nil {
		t.Fatalf("ListPreviews: %v", err)
	}
	if len(previews) != 1 {
		t.Errorf("len = %d, want 1", len(previews))
	}

	s.DeletePreview(appName, prNumber)
	_, err = s.GetPreview(appName, prNumber)
	if err == nil {
		t.Error("expected error after remove")
	}
}

func TestStoreListApps(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	if err := s.SaveApp(types.AppEntry{Name: "app1", Port: 9000}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveApp(types.AppEntry{Name: "app2", Port: 9001}); err != nil {
		t.Fatal(err)
	}

	apps, err := s.ListApps()
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}
}

func TestStoreFreeOrphanedPorts(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_ = s.SaveApp(types.AppEntry{Name: "app1", Port: 9000})
	_, _ = s.AllocatePort("app1")
	_, _ = s.AllocatePort("orphan-app")

	freed, err := s.FreeOrphanedPorts()
	if err != nil {
		t.Fatalf("FreeOrphanedPorts() error = %v", err)
	}
	if freed != 1 {
		t.Errorf("expected 1 orphaned port, got %d", freed)
	}
}

func TestPruneBuildLogs(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("v%d", i+1)
		s.SaveBuildLog("testapp", id, "content")
		time.Sleep(1 * time.Millisecond)
	}

	if err := s.PruneBuildLogs("testapp", 3); err != nil {
		t.Fatalf("PruneBuildLogs() error = %v", err)
	}

	logs, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatalf("ListBuildLogs() error = %v", err)
	}
	if len(logs) > 3 {
		t.Errorf("expected at most 3 build logs after prune, got %d: %v", len(logs), logs)
	}
}
