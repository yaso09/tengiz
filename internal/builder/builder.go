package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type Builder struct {
	dataDir  string
	strategy Strategy
}

func New(dataDir string) *Builder {
	return &Builder{dataDir: dataDir}
}

func (b *Builder) SetStrategy(s Strategy) {
	b.strategy = s
}

func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if b.strategy != nil {
		return b.strategy.Build(ctx, dir, appName, env, deploymentID, detection)
	}
	if detection.Framework == FrameworkDocker {
		return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
	}
	if err := b.ensureDockerfile(dir, detection); err != nil {
		return "", "", fmt.Errorf("generate dockerfile: %w", err)
	}
	return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}

func (b *Builder) ensureDockerfile(dir string, detection *Detection) error {
	dfPath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dfPath); err == nil {
		return nil
	}
	content := generateDockerfile(detection)
	return os.WriteFile(dfPath, []byte(content), 0644)
}

func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}

func generateDockerfile(d *Detection) string {
	port := d.InternalPort
	if port == 0 {
		port = 8080
	}

	var df string
	switch d.Framework {
	case FrameworkNextJS:
		df = fmt.Sprintf(`FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-alpine
WORKDIR /app
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
EXPOSE %d
CMD ["npm", "start"]`, port)
	case FrameworkVite:
		df = fmt.Sprintf(`FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]`, port)
	case FrameworkGo:
		df = fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE %d
CMD ["./app"]`, port)
	case FrameworkNode:
		df = fmt.Sprintf(`FROM node:22-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
EXPOSE %d
CMD ["npm", "start"]`, port)
	case FrameworkPython:
		df = fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE %d
CMD ["python", "app.py"]`, port)
	case FrameworkStatic:
		df = fmt.Sprintf(`FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]`, port)
	default:
		df = fmt.Sprintf(`FROM alpine
EXPOSE %d
CMD ["echo", "no dockerfile generated for this framework"]`, port)
	}

	if d.HealthCheck != nil && d.HealthCheck.Enabled {
		endpoint := d.HealthCheck.Endpoint
		if endpoint == "" {
			endpoint = "/health"
		}
		interval := d.HealthCheck.Interval
		if interval <= 0 {
			interval = 30
		}
		timeout := d.HealthCheck.Timeout
		if timeout <= 0 {
			timeout = 5
		}
		retries := d.HealthCheck.Retries
		if retries <= 0 {
			retries = 3
		}
		df += fmt.Sprintf("\nHEALTHCHECK --interval=%ds --timeout=%ds --start-period=%ds --retries=%d CMD curl -f http://localhost:%d%s || exit 1\n",
			interval, timeout, d.HealthCheck.StartPeriod, retries, port, endpoint)
	}

	return df
}
