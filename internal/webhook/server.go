package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"strings"
)

type DeployFunc func(ctx context.Context, repoURL, branch, provider string) error

type PreviewFunc func(appName string, prNumber int, branch, repoURL string) error

type PreviewDeployFunc func(ctx context.Context, repoURL string, prNumber int, branch string) error

type PreviewCleanupFunc func(ctx context.Context, repoURL string, prNumber int) error

type Config struct {
	Secret          string   `yaml:"secret"`
	AllowedBranches []string `yaml:"allowed_branches"`
	Port            int      `yaml:"port"`
}

type Server struct {
	dataDir          string
	cfg              *Config
	deployFn         DeployFunc
	previewFn        PreviewFunc
	previewDeployFn  PreviewDeployFunc
	previewCleanupFn PreviewCleanupFunc
	httpServer       *http.Server
}

func New(dataDir string, cfg *Config, fn DeployFunc) *Server {
	return &Server{
		dataDir:  dataDir,
		cfg:      cfg,
		deployFn: fn,
	}
}

func NewWithPreview(dataDir string, cfg *Config, fn DeployFunc, previewDeployFn PreviewDeployFunc, previewCleanupFn PreviewCleanupFunc) *Server {
	s := &Server{
		dataDir:          dataDir,
		cfg:              cfg,
		deployFn:         fn,
		previewDeployFn:  previewDeployFn,
		previewCleanupFn: previewCleanupFn,
	}
	// Wire the new-style preview functions into the existing PreviewFunc handler
	s.previewFn = func(appName string, prNumber int, branch, repoURL string) error {
		if branch == "" {
			return previewCleanupFn(context.Background(), repoURL, prNumber)
		}
		return previewDeployFn(context.Background(), repoURL, prNumber, branch)
	}
	return s
}

func (s *Server) SetPreviewFunc(fn PreviewFunc) {
	s.previewFn = fn
}

func (s *Server) Start(ctx context.Context, port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.webhookHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[tengiz] webhook server listening on :%d", port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return s.httpServer.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (s *Server) webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Determine provider from headers
	var provider string
	switch {
	case r.Header.Get("X-Github-Event") != "":
		provider = "github"
	case r.Header.Get("X-Gitlab-Event") != "":
		provider = "gitlab"
	case r.Header.Get("X-Hook-UUID") != "":
		provider = "bitbucket"
	case r.Header.Get("X-Gitea-Event") != "":
		provider = "gitea"
	default:
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	// Handle ping events
	if r.Header.Get("X-Github-Event") == "ping" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","event":"ping"}`))
		return
	}

	// Handle pull_request events (GitHub)
	eventType := r.Header.Get("X-Github-Event")
	if eventType == "pull_request" {
		s.handlePullRequest(w, r, body)
		return
	}

	// Only process push events
	if eventType == "" {
		eventType = r.Header.Get("X-Gitlab-Event")
	}
	if eventType == "" {
		eventType = "push" // Bitbucket/Gitea don't send event type header; assume push
	}
	if eventType != "push" && eventType != "Push Hook" && !strings.HasPrefix(eventType, "push") {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored","event":"` + eventType + `"}`))
		return
	}

	// Verify HMAC if secret is configured
	if err := s.verifyHMAC(r, body); err != nil {
		log.Printf("[tengiz] webhook HMAC verification failed: %v", err)
		http.Error(w, "signature verification failed", http.StatusForbidden)
		return
	}

	var repo, ref string

	switch provider {
	case "github":
		repo, ref, _, err = parseGitHubEvent(r, body)
	case "gitlab":
		repo, ref, _, err = parseGitLabEvent(r, body)
	case "bitbucket":
		repo, ref, _, err = parseBitbucketEvent(r, body)
	case "gitea":
		repo, ref, _, err = parseGiteaEvent(r, body)
	}

	if err != nil {
		log.Printf("[tengiz] webhook parse error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	branch := strings.TrimPrefix(ref, "refs/heads/")

	// Branch filtering
	if !s.isBranchAllowed(branch) {
		log.Printf("[tengiz] webhook: branch %q not in allowed list, skipping", branch)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"skipped","reason":"branch not allowed"}`))
		return
	}

	log.Printf("[tengiz] webhook: %s push to %s/%s", provider, repo, branch)

	if s.deployFn != nil {
		if err := s.deployFn(r.Context(), repo, branch, provider); err != nil {
			log.Printf("[tengiz] deploy error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handlePullRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int `json:"number"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			CloneURL string `json:"clone_url"`
			Name     string `json:"name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[tengiz] webhook: invalid pull_request payload: %v", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	appName := payload.Repository.Name
	prNumber := payload.PullRequest.Number
	branch := payload.PullRequest.Head.Ref
	repoURL := payload.Repository.CloneURL

	log.Printf("[tengiz] webhook: pull_request %s for %s PR #%d (%s)", payload.Action, appName, prNumber, branch)

	if s.previewFn == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored","reason":"no preview handler configured"}`))
		return
	}

	switch payload.Action {
	case "opened", "reopened", "synchronize":
		if err := s.previewFn(appName, prNumber, branch, repoURL); err != nil {
			log.Printf("[tengiz] preview error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "closed":
		if err := s.previewFn(appName, prNumber, "", repoURL); err != nil {
			log.Printf("[tengiz] preview cleanup error: %v", err)
		}
	default:
		log.Printf("[tengiz] webhook: ignoring pull_request action %q", payload.Action)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) verifyHMAC(r *http.Request, body []byte) error {
	if s.cfg == nil || s.cfg.Secret == "" {
		return nil // no secret configured = skip verification
	}

	secret := []byte(s.cfg.Secret)
	var providedSig string
	var hashFunc func() hash.Hash

	switch {
	case r.Header.Get("X-Github-Event") != "" || r.Header.Get("X-Gitea-Event") != "":
		// GitHub/Gitea: X-Hub-Signature-256
		providedSig = r.Header.Get("X-Hub-Signature-256")
		hashFunc = sha256.New
	case r.Header.Get("X-Gitlab-Event") != "":
		// GitLab: X-Gitlab-Token (plain text comparison)
		providedToken := r.Header.Get("X-Gitlab-Token")
		if hmac.Equal([]byte(providedToken), secret) {
			return nil
		}
		return fmt.Errorf("gitlab token mismatch")
	case r.Header.Get("X-Hook-UUID") != "":
		// Bitbucket: X-Hub-Signature (HMAC-SHA256)
		providedSig = r.Header.Get("X-Hub-Signature")
		hashFunc = sha256.New
	default:
		return nil
	}

	if providedSig == "" {
		return fmt.Errorf("missing signature header")
	}

	mac := hmac.New(hashFunc, secret)
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func (s *Server) isBranchAllowed(branch string) bool {
	if s.cfg == nil || len(s.cfg.AllowedBranches) == 0 {
		return true // empty list = allow all
	}
	for _, allowed := range s.cfg.AllowedBranches {
		if branch == allowed {
			return true
		}
	}
	return false
}

func parseGitHubEvent(r *http.Request, body []byte) (repo, ref, provider string, err error) {
	var payload struct {
		Ref        string `json:"ref"`
		Repository struct {
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", "", fmt.Errorf("github: %w", err)
	}
	return payload.Repository.CloneURL, payload.Ref, "github", nil
}

func parseGitLabEvent(r *http.Request, body []byte) (repo, ref, provider string, err error) {
	var payload struct {
		Ref     string `json:"ref"`
		Project struct {
			GitHTTPURL string `json:"git_http_url"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", "", fmt.Errorf("gitlab: %w", err)
	}
	return payload.Project.GitHTTPURL, payload.Ref, "gitlab", nil
}

func parseBitbucketEvent(r *http.Request, body []byte) (repo, ref, provider string, err error) {
	var payload struct {
		Push struct {
			Changes []struct {
				New struct {
					Name string `json:"name"`
				} `json:"new"`
			} `json:"changes"`
		} `json:"push"`
		Repository struct {
			Links struct {
				Clone []struct {
					Href string `json:"href"`
				} `json:"clone"`
			} `json:"links"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", "", fmt.Errorf("bitbucket: %w", err)
	}
	if len(payload.Push.Changes) == 0 {
		return "", "", "", fmt.Errorf("bitbucket: no changes in push event")
	}
	branch := payload.Push.Changes[0].New.Name
	cloneURL := ""
	for _, c := range payload.Repository.Links.Clone {
		if strings.HasPrefix(c.Href, "https://") {
			cloneURL = c.Href
			break
		}
	}
	return cloneURL, "refs/heads/" + branch, "bitbucket", nil
}

func parseGiteaEvent(r *http.Request, body []byte) (repo, ref, provider string, err error) {
	repo, ref, _, err = parseGitHubEvent(r, body)
	return repo, ref, "gitea", err
}

func parseGitHubPREvent(body []byte) (repo string, prNumber int, branch string, action string, err error) {
	var payload struct {
		Action string `json:"action"`
		Number int    `json:"number"`
		PullRequest struct {
			Head struct {
				Ref  string `json:"ref"`
				Repo struct {
					CloneURL string `json:"clone_url"`
				} `json:"repo"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", 0, "", "", fmt.Errorf("pull_request: %w", err)
	}
	repo = payload.Repository.CloneURL
	if repo == "" {
		repo = payload.PullRequest.Head.Repo.CloneURL
	}
	return repo, payload.Number, payload.PullRequest.Head.Ref, payload.Action, nil
}
