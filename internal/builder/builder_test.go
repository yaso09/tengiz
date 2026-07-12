package builder

import (
	"os"
	"path/filepath"
	"testing"
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
