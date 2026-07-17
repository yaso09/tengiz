package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNixpacksBuildAndDockerBuildIntegration(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks not installed")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}

	b := New(t.TempDir())
	dir := t.TempDir()
	// Create a minimal Node.js project (nixpacks detects this)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
	 "name": "test-app",
	 "version": "1.0.0",
	 "scripts": { "start": "node index.js" }
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "index.js"), []byte(`const http = require('http');
const server = http.createServer((req, res) => {
  res.writeHead(200);
  res.end('hello');
});
server.listen(process.env.PORT || 3000);
`), 0644)

	detection := &Detection{
		Framework:    FrameworkNixpacks,
		InternalPort: 3000,
		Builder:      "nixpacks",
	}

	tag, logs, err := b.Build(context.Background(), dir, "test-nixpacks-integration", "test", detection, "int-v1")
	if err != nil {
		t.Fatalf("Build() error: %v\nlogs:\n%s", err, logs)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}

	// Verify the image exists
	inspect := exec.CommandContext(context.Background(), "docker", "inspect", tag)
	if out, err := inspect.CombinedOutput(); err != nil {
		t.Fatalf("docker inspect failed: %v\n%s", err, out)
	}
}
