package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type DopplerConfig struct {
	Token   string
	Project string
	Config  string
}

type DopplerProvider struct {
	token   string
	project string
	config  string
	client  *http.Client
}

func NewDopplerProvider(cfg DopplerConfig) (*DopplerProvider, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("doppler token is required")
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("doppler project is required")
	}
	if cfg.Config == "" {
		return nil, fmt.Errorf("doppler config is required")
	}
	return &DopplerProvider{
		token:   cfg.Token,
		project: cfg.Project,
		config:  cfg.Config,
		client:  http.DefaultClient,
	}, nil
}

func (p *DopplerProvider) Name() string { return "doppler" }

func (p *DopplerProvider) apiURL(path string) string {
	return fmt.Sprintf("https://api.doppler.com/v3%s", path)
}

func (p *DopplerProvider) doReq(method, url string, body interface{}) (*http.Response, error) {
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	return p.client.Do(req)
}

func (p *DopplerProvider) secretName(appName, key string) string {
	return fmt.Sprintf("%s_%s", strings.ToUpper(appName), key)
}

func (p *DopplerProvider) Set(appName, key, value string) error {
	name := p.secretName(appName, key)
	body := map[string]interface{}{
		"project": p.project,
		"config":  p.config,
		"name":    name,
		"value":   value,
	}
	resp, err := p.doReq("PUT", p.apiURL("/configs/config/secret"), body)
	if err != nil {
		return fmt.Errorf("doppler set: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("doppler set: status %d", resp.StatusCode)
	}
	return nil
}

type dopplerSecretsResponse struct {
	Secrets map[string]struct {
		RawValue string `json:"raw_value"`
	} `json:"secrets"`
	Success bool `json:"success"`
}

func (p *DopplerProvider) Get(appName, key string) (string, bool, error) {
	name := p.secretName(appName, key)
	url := p.apiURL(fmt.Sprintf("/configs/config/secret?project=%s&config=%s&name=%s",
		p.project, p.config, name))
	resp, err := p.doReq("GET", url, nil)
	if err != nil {
		return "", false, fmt.Errorf("doppler get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", false, nil
	}
	if resp.StatusCode >= 400 {
		return "", false, fmt.Errorf("doppler get: status %d", resp.StatusCode)
	}
	var result dopplerSecretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("doppler decode: %w", err)
	}
	if s, ok := result.Secrets[name]; ok {
		return s.RawValue, true, nil
	}
	return "", false, nil
}

func (p *DopplerProvider) Unset(appName, key string) error {
	name := p.secretName(appName, key)
	url := p.apiURL(fmt.Sprintf("/configs/config/secret?project=%s&config=%s&name=%s",
		p.project, p.config, name))
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("doppler delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		return fmt.Errorf("doppler delete: status %d", resp.StatusCode)
	}
	return nil
}

func (p *DopplerProvider) List(appName string) (map[string]string, error) {
	url := p.apiURL(fmt.Sprintf("/configs/config/secrets?project=%s&config=%s",
		p.project, p.config))
	resp, err := p.doReq("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("doppler list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("doppler list: status %d", resp.StatusCode)
	}
	var result dopplerSecretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("doppler decode: %w", err)
	}
	prefix := strings.ToUpper(appName) + "_"
	secrets := make(map[string]string, len(result.Secrets))
	for name, s := range result.Secrets {
		if strings.HasPrefix(name, prefix) {
			key := strings.TrimPrefix(name, prefix)
			secrets[key] = s.RawValue
		}
	}
	return secrets, nil
}
