package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	s := New("/tmp/test-tengiz", fn)
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
	t.Skip("bitbucket parser not yet implemented")
}

func TestParseGiteaEvent(t *testing.T) {
	t.Skip("gitea parser not yet implemented")
}
