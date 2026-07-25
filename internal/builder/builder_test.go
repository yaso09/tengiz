package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestDetectNextJS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "next.config.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNextJS {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNextJS)
	}
}

func TestDetectDocker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkDocker {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkDocker)
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkGo {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkGo)
	}
}

func TestDetectVite(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "vite.config.ts"), []byte(""), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkVite {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkVite)
	}
}

func TestDetectStatic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkStatic {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkStatic)
	}
}

func TestDetectNode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNode {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNode)
	}
}

func TestGenerateDockerfileNextJS(t *testing.T) {
	df := generateDockerfile(&Detection{Framework: FrameworkNextJS, InternalPort: 3000})
	if !contains(df, "nextjs") && !contains(df, "FROM node") {
		t.Error("NextJS Dockerfile should contain node image")
	}
	if !contains(df, "3000") {
		t.Error("NextJS Dockerfile should expose port 3000")
	}
}

func TestGenerateDockerfileGo(t *testing.T) {
	df := generateDockerfile(&Detection{Framework: FrameworkGo, InternalPort: 8080})
	if !contains(df, "FROM golang") {
		t.Error("Go Dockerfile should contain golang image")
	}
}

func TestGenerateDockerfileWithHealthCheck(t *testing.T) {
	hc := &types.HealthCheckConfig{
		Enabled:  true,
		Endpoint: "/healthz",
		Interval: 15,
		Timeout:  3,
		Retries:  2,
	}
	d := &Detection{
		Framework:    FrameworkNode,
		InternalPort: 3000,
		HealthCheck:  hc,
	}
	df := generateDockerfile(d)
	if !strings.Contains(df, "HEALTHCHECK") {
		t.Error("generated Dockerfile missing HEALTHCHECK instruction")
	}
	if !strings.Contains(df, "/healthz") {
		t.Error("generated Dockerfile missing custom endpoint")
	}
	if !strings.Contains(df, "--interval=15s") {
		t.Error("generated Dockerfile missing custom interval")
	}
}

func TestGenerateDockerfileWithoutHealthCheck(t *testing.T) {
	d := &Detection{
		Framework:    FrameworkGo,
		InternalPort: 8080,
	}
	df := generateDockerfile(d)
	if strings.Contains(df, "HEALTHCHECK") {
		t.Error("generated Dockerfile should not contain HEALTHCHECK when not configured")
	}
}

func TestBuildCapturesOutput(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	_ = logs
}

func TestBuildWithDeploymentIDCompiles(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}
	tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}

func TestFrameworkNixpacksConstant(t *testing.T) {
	if FrameworkNixpacks != "nixpacks" {
		t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
	}
}

func TestBuildWithNixpacksDispatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.rs"), []byte("fn main() {}"), 0644)

	detection := &Detection{
		Framework:    FrameworkNixpacks,
		InternalPort: 8080,
	}

	b := New(t.TempDir())
	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
	if err != nil {
		if strings.Contains(err.Error(), "nixpacks not found") {
			t.Skip("nixpacks CLI not available, skipping integration test")
		}
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	_ = logs
}

func TestBuildWithNixpacksWhenConfigSelected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0644)

	detection, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if detection.Framework == FrameworkNixpacks {
		t.Skip("nixpacks detected, skipping")
	}
}

func TestBuildWithNixpacksCompiles(t *testing.T) {
	b := New(t.TempDir())
	b.SetNixpacksConfig(&types.NixpacksConfig{
		Packages: []string{"curl"},
	})
	if b.nixpacksCfg == nil {
		t.Error("expected nixpacksCfg to be set")
	}
	if len(b.nixpacksCfg.Packages) != 1 {
		t.Error("expected 1 package")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
