package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type eventCase struct {
	name     string
	event    string
	body     interface{}
	wantRepo string
	wantRef  string
}

func TestParseGitHubPushEvent(t *testing.T) {
	cases := []eventCase{
		{
			name:  "github push main",
			event: "push",
			body: map[string]interface{}{
				"repository": map[string]interface{}{
					"clone_url": "https://github.com/user/myapp.git",
				},
				"ref": "refs/heads/main",
			},
			wantRepo: "https://github.com/user/myapp.git",
			wantRef:  "refs/heads/main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
			req.Header.Set("X-Github-Event", tc.event)

			repo, ref, provider, err := parseGitHubEvent(req, body)
			if err != nil {
				t.Fatalf("parseGitHubEvent: %v", err)
			}
			if repo != tc.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tc.wantRepo)
			}
			if ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", ref, tc.wantRef)
			}
			if provider != "github" {
				t.Errorf("provider = %q, want github", provider)
			}
		})
	}
}

func TestParseGitLabPushEvent(t *testing.T) {
	body := map[string]interface{}{
		"project": map[string]interface{}{
			"git_http_url": "https://gitlab.com/user/myapp.git",
		},
		"ref": "refs/heads/main",
	}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(bodyJSON))
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	repo, ref, provider, err := parseGitLabEvent(req, bodyJSON)
	if err != nil {
		t.Fatalf("parseGitLabEvent: %v", err)
	}
	if repo != "https://gitlab.com/user/myapp.git" {
		t.Errorf("repo = %q", repo)
	}
	if ref != "refs/heads/main" {
		t.Errorf("ref = %q", ref)
	}
	if provider != "gitlab" {
		t.Errorf("provider = %q", provider)
	}
}

func TestWebhookDispatch(t *testing.T) {
	deployed := make(chan string, 1)
	fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
		deployed <- repo
		return nil
	})

	s := New("/tmp/test-tengiz", nil, fn)
	srv := httptest.NewServer(http.HandlerFunc(s.webhookHandler))
	defer srv.Close()

	body := map[string]interface{}{
		"repository": map[string]interface{}{
			"clone_url": "https://github.com/user/myapp.git",
		},
		"ref": "refs/heads/main",
	}
	reqBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL, bytes.NewReader(reqBody))
	req.Header.Set("X-Github-Event", "push")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	got := <-deployed
	if got != "https://github.com/user/myapp.git" {
		t.Errorf("deployed repo = %q", got)
	}
}

func TestParseBitbucketEvent(t *testing.T) {
	body := map[string]interface{}{
		"push": map[string]interface{}{
			"changes": []interface{}{
				map[string]interface{}{
					"new": map[string]interface{}{
						"name": "main",
					},
				},
			},
		},
		"repository": map[string]interface{}{
			"links": map[string]interface{}{
				"clone": []interface{}{
					map[string]interface{}{
						"href": "https://bitbucket.org/user/myapp.git",
						"name": "https",
					},
				},
			},
		},
	}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(bodyJSON))
	req.Header.Set("X-Hook-UUID", "some-uuid")

	repo, ref, provider, err := parseBitbucketEvent(req, bodyJSON)
	if err != nil {
		t.Fatalf("parseBitbucketEvent: %v", err)
	}
	if repo != "https://bitbucket.org/user/myapp.git" {
		t.Errorf("repo = %q, want https://bitbucket.org/user/myapp.git", repo)
	}
	if ref != "refs/heads/main" {
		t.Errorf("ref = %q, want refs/heads/main", ref)
	}
	if provider != "bitbucket" {
		t.Errorf("provider = %q, want bitbucket", provider)
	}
}

func TestParseGiteaEvent(t *testing.T) {
	body := map[string]interface{}{
		"repository": map[string]interface{}{
			"clone_url": "https://gitea.com/user/myapp.git",
		},
		"ref": "refs/heads/main",
	}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(bodyJSON))
	req.Header.Set("X-Gitea-Event", "push")

	repo, ref, provider, err := parseGiteaEvent(req, bodyJSON)
	if err != nil {
		t.Fatalf("parseGiteaEvent: %v", err)
	}
	if repo != "https://gitea.com/user/myapp.git" {
		t.Errorf("repo = %q, want https://gitea.com/user/myapp.git", repo)
	}
	if ref != "refs/heads/main" {
		t.Errorf("ref = %q, want refs/heads/main", ref)
	}
	if provider != "gitea" {
		t.Errorf("provider = %q, want gitea", provider)
	}
}

func TestHMACVerification(t *testing.T) {
	secret := "my-webhook-secret-123"
	body := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	cfg := &Config{Secret: secret}
	s := &Server{cfg: cfg}

	// Test valid signature
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", expectedSig)
	req.Header.Set("X-Github-Event", "push")

	if err := s.verifyHMAC(req, body); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}

	// Test invalid signature
	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req2.Header.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	req2.Header.Set("X-Github-Event", "push")

	if err := s.verifyHMAC(req2, body); err == nil {
		t.Error("invalid signature accepted")
	}

	// Test missing signature (no secret configured = accept)
	s2 := &Server{cfg: nil}
	req3 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	if err := s2.verifyHMAC(req3, body); err != nil {
		t.Errorf("no secret configured should accept: %v", err)
	}
}

func TestPingEvent(t *testing.T) {
	deployed := make(chan string, 1)
	fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
		deployed <- repo
		return nil
	})

	cfg := &Config{
		AllowedBranches: []string{"main"},
	}
	s := &Server{cfg: cfg, deployFn: fn}

	body := []byte(`{"zen":"keep it simple","hook_id":123456}`)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Github-Event", "ping")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ping status = %d, want 200", w.Code)
	}

	select {
	case <-deployed:
		t.Error("ping event triggered a deploy")
	default:
		// OK — no deploy triggered
	}
}

func TestNonPushEventIgnored(t *testing.T) {
	deployed := make(chan string, 1)
	fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
		deployed <- repo
		return nil
	})

	s := &Server{deployFn: fn}

	body := []byte(`{"action":"opened","number":1,"repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Github-Event", "pull_request")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("non-push status = %d, want 200", w.Code)
	}

	select {
	case <-deployed:
		t.Error("pull_request event triggered a deploy")
	default:
		// OK
	}
}

func TestBranchFiltering(t *testing.T) {
	deployed := make(chan struct{ name, branch string }, 1)
	fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
		deployed <- struct{ name, branch string }{repo, branch}
		return nil
	})

	cfg := &Config{
		AllowedBranches: []string{"main", "production"},
	}
	s := &Server{cfg: cfg, deployFn: fn}

	// Push to "develop" — should be ignored
	body := []byte(`{"ref":"refs/heads/develop","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Github-Event", "push")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("filtered branch status = %d, want 200", w.Code)
	}

	select {
	case <-deployed:
		t.Error("develop branch should have been filtered out")
	default:
		// OK
	}

	// Push to "main" — should trigger deploy
	body2 := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body2))
	req2.Header.Set("X-Github-Event", "push")

	w2 := httptest.NewRecorder()
	s.webhookHandler(w2, req2)

	select {
	case dep := <-deployed:
		if dep.branch != "main" {
			t.Errorf("deployed branch = %q, want main", dep.branch)
		}
	default:
		t.Error("main branch should have triggered a deploy")
	}
}

func TestAllowedBranchesAll(t *testing.T) {
	// Empty AllowedBranches = allow all
	deployed := make(chan string, 1)
	fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
		deployed <- branch
		return nil
	})

	cfg := &Config{} // nil/empty AllowedBranches = allow all
	s := &Server{cfg: cfg, deployFn: fn}

	body := []byte(`{"ref":"refs/heads/any-branch","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Github-Event", "push")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	select {
	case branch := <-deployed:
		if branch != "any-branch" {
			t.Errorf("branch = %q, want any-branch", branch)
		}
	default:
		t.Error("any branch should trigger deploy when AllowedBranches is empty")
	}
}

func TestGitLabHMACVerification(t *testing.T) {
	secret := "gitlab-token-42"
	cfg := &Config{Secret: secret}
	s := &Server{cfg: cfg}

	body := []byte(`{"ref":"refs/heads/main","project":{"git_http_url":"https://gitlab.com/user/myapp.git"}}`)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", secret)
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	if err := s.verifyHMAC(req, body); err != nil {
		t.Errorf("GitLab valid token rejected: %v", err)
	}

	// Wrong token
	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req2.Header.Set("X-Gitlab-Token", "wrong-token")
	req2.Header.Set("X-Gitlab-Event", "Push Hook")

	if err := s.verifyHMAC(req2, body); err == nil {
		t.Error("GitLab invalid token accepted")
	}
}

func TestBitbucketHMACVerification(t *testing.T) {
	secret := "bitbucket-secret"
	cfg := &Config{Secret: secret}
	s := &Server{cfg: cfg}

	body := []byte(`{"push":{"changes":[{"new":{"name":"main"}}]},"repository":{"links":{"clone":[{"href":"https://bitbucket.org/user/myapp.git","name":"https"}]}}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature", expectedSig)
	req.Header.Set("X-Hook-UUID", "some-uuid")

	if err := s.verifyHMAC(req, body); err != nil {
		t.Errorf("Bitbucket valid signature rejected: %v", err)
	}
}

func TestGiteaHMACVerification(t *testing.T) {
	secret := "gitea-secret"
	cfg := &Config{Secret: secret}
	s := &Server{cfg: cfg}

	body := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://gitea.com/user/myapp.git"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", expectedSig)
	req.Header.Set("X-Gitea-Event", "push")

	if err := s.verifyHMAC(req, body); err != nil {
		t.Errorf("Gitea valid signature rejected: %v", err)
	}
}

func TestPullRequestOpenedEvent(t *testing.T) {
	previewCh := make(chan struct {
		appName  string
		prNumber int
		branch   string
		repoURL  string
	}, 1)

	s := New("", nil, nil)
	s.SetPreviewFunc(func(appName string, prNumber int, branch, repoURL string) error {
		previewCh <- struct {
			appName  string
			prNumber int
			branch   string
			repoURL  string
		}{appName, prNumber, branch, repoURL}
		return nil
	})

	body := `{
		"action": "opened",
		"pull_request": {
			"number": 42,
			"head": { "ref": "feature/awesome" }
		},
		"repository": {
			"clone_url": "https://github.com/user/myapp.git",
			"name": "myapp"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-Github-Event", "pull_request")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	select {
	case ev := <-previewCh:
		if ev.prNumber != 42 {
			t.Errorf("prNumber = %d, want 42", ev.prNumber)
		}
		if ev.branch != "feature/awesome" {
			t.Errorf("branch = %q, want %q", ev.branch, "feature/awesome")
		}
		if ev.appName != "myapp" {
			t.Errorf("appName = %q, want %q", ev.appName, "myapp")
		}
	case <-time.After(time.Second):
		t.Error("previewFn was not called")
	}
}

func TestPullRequestClosedEvent(t *testing.T) {
	cleanupCh := make(chan struct {
		appName  string
		prNumber int
	}, 1)

	s := New("", nil, nil)
	s.SetPreviewFunc(func(appName string, prNumber int, branch, repoURL string) error {
		cleanupCh <- struct {
			appName  string
			prNumber int
		}{appName, prNumber}
		return nil
	})

	body := `{
		"action": "closed",
		"pull_request": {
			"number": 42,
			"head": { "ref": "feature/awesome" }
		},
		"repository": {
			"clone_url": "https://github.com/user/myapp.git",
			"name": "myapp"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-Github-Event", "pull_request")

	w := httptest.NewRecorder()
	s.webhookHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	select {
	case ev := <-cleanupCh:
		if ev.prNumber != 42 {
			t.Errorf("prNumber = %d, want 42", ev.prNumber)
		}
	case <-time.After(time.Second):
		t.Error("previewFn was not called for closed event")
	}
}

