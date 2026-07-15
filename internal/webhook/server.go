package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type DeployFunc func(ctx context.Context, repoURL, branch, provider string) error

type Server struct {
	dataDir    string
	deployFn   DeployFunc
	httpServer *http.Server
}

func New(dataDir string, fn DeployFunc) *Server {
	return &Server{
		dataDir:  dataDir,
		deployFn: fn,
	}
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

	var repo, ref, provider string

	switch {
	case r.Header.Get("X-Github-Event") != "":
		repo, ref, provider, err = parseGitHubEvent(r, body)
	case r.Header.Get("X-Gitlab-Event") != "":
		repo, ref, provider, err = parseGitLabEvent(r, body)
	case r.Header.Get("X-Hook-UUID") != "":
		repo, ref, provider, err = parseBitbucketEvent(r, body)
	case r.Header.Get("X-Gitea-Event") != "":
		repo, ref, provider, err = parseGiteaEvent(r, body)
	default:
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Printf("[tengiz] webhook parse error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	branch := strings.TrimPrefix(ref, "refs/heads/")
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
