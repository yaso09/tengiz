package types

import "time"

type PreviewStatus string

const (
	PreviewActive   PreviewStatus = "active"
	PreviewCleanup  PreviewStatus = "cleanup"
	PreviewDeleting PreviewStatus = "deleting"
	PreviewFailed   PreviewStatus = "failed"
)

type PreviewEntry struct {
	AppName       string    `json:"app_name"`
	PRNumber      int       `json:"pr_number"`
	Branch        string    `json:"branch"`
	RepoURL       string    `json:"repo_url"`
	ImageTag      string    `json:"image_tag"`
	Port          int       `json:"port"`
	ContainerName string    `json:"container_name"`
	DeploymentID  string    `json:"deployment_id"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
