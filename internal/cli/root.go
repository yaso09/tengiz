package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/gitdeploy"
	"github.com/yaso09/tengiz/internal/health"
	"github.com/yaso09/tengiz/internal/notify"
	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/preview"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/secrets"
	"github.com/yaso09/tengiz/internal/types"
	"github.com/yaso09/tengiz/internal/webhook"
)

	var dataDir string

func init() {
	home, _ := os.UserHomeDir()
	dataDir = filepath.Join(home, ".tengiz")
	rootCmd.PersistentFlags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(devCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configUnsetCmd)
	configCmd.AddCommand(configShowCmd)
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainRemoveCmd)
	domainCmd.AddCommand(domainListCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(domainCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(webhookCmd)
	gitCmd.AddCommand(gitConnectCmd)
	gitCmd.AddCommand(gitDisconnectCmd)
	rootCmd.AddCommand(gitCmd)
	volumeCmd.AddCommand(volumeAddCmd)
	volumeCmd.AddCommand(volumeRemoveCmd)
	volumeCmd.AddCommand(volumeListCmd)
	rootCmd.AddCommand(volumeCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(buildLogsCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (unreferenced) images")
	cleanupCmd.Flags().Bool("all-images", false, "prune all unused images (not just dangling)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (data loss risk)")
	cleanupCmd.Flags().Bool("all", false, "clean everything, including unused volumes")
	secretCmd.AddCommand(secretSetCmd, secretGetCmd, secretUnsetCmd, secretListCmd, secretRotateCmd)
	rootCmd.AddCommand(secretCmd)
	notificationCmd.AddCommand(notificationEnableCmd)
	notificationCmd.AddCommand(notificationDisableCmd)
	notificationCmd.AddCommand(notificationConfigCmd)
	notificationCmd.AddCommand(notificationSetChannelCmd)
	notificationCmd.AddCommand(notificationShowCmd)
	rootCmd.AddCommand(notificationCmd)
	deployCmd.Flags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
	runCmd.Flags().BoolP("interactive", "i", false, "enable interactive TTY mode")
	runCmd.Flags().StringArrayP("env", "e", nil, "set additional env vars (can be repeated: -e KEY=VALUE)")
	initCmd.Flags().String("git-repo", "", "git repository URL for auto-deploy")
	initCmd.Flags().String("git-branch", "main", "git branch for auto-deploy")
	logsCmd.Flags().BoolP("follow", "f", false, "follow log output")
	logsCmd.Flags().Int("tail", 0, "show only last N lines of logs (0 = all)")
	logsCmd.Flags().String("since", "", "show logs since timestamp (e.g. 5m, 2h, 2024-01-01T00:00:00Z)")
	logsCmd.Flags().String("until", "", "show logs before timestamp (e.g. 5m, 2h, 2024-01-01T00:00:00Z)")
	logsCmd.Flags().String("grep", "", "filter logs with a case-sensitive pattern (client-side)")
	webhookCmd.Flags().IntP("port", "p", 9090, "webhook listen port")
	webhookCmd.Flags().String("env", "production", "deployment environment for auto-deploys")
	webhookCmd.Flags().String("config", "", "path to .tengiz.yaml for webhook configuration")
}

var rootCmd = &cobra.Command{
	Use:   "tengiz",
	Short: "Tengiz - Serverless deployment platform",
	Long:  "Tengiz is a Vercel alternative. Deploy any app with scale-to-zero.",
}

func getEnv(cmd *cobra.Command) string {
	env, _ := cmd.Flags().GetString("env")
	if env == "" {
		return "production"
	}
	return env
}

var initCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Create a .tengiz.yaml configuration file",
	Long:  "Creates a .tengiz.yaml in the current directory with an optional app name. Prompts for missing values.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := filepath.Base(getwd())
		if len(args) > 0 {
			name = args[0]
		}

		path := ".tengiz.yaml"
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf(".tengiz.yaml already exists")
		}

		env := getEnv(cmd)
		gitRepo, _ := cmd.Flags().GetString("git-repo")
		gitBranch, _ := cmd.Flags().GetString("git-branch")

		content := fmt.Sprintf(`name: %s
environment: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# healthcheck:
#   enabled: true
#   endpoint: /health
#   port: 3000
#   interval: 30
#   retries: 3
#   timeout: 5
#   start_period: 0
# volumes:
#   - host_path: /data/myapp
#     container_path: /app/data
#     read_only: false
# domains:
#   - app.example.com
# env:
#   DATABASE_URL: postgres://localhost:5432/myapp
#   API_KEY: your-secret-key
# resources:
#   cpu: "1.0"           # CPU cores (e.g., "0.5", "2")
#   memory: "256m"       # memory limit (e.g., "128m", "1g")
`, name, env)

		if gitRepo != "" {
			content += fmt.Sprintf("git:\n  repo: %s\n  branch: %s\n", gitRepo, gitBranch)
		}

		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write .tengiz.yaml: %w", err)
		}

		fmt.Printf("[tengiz] created .tengiz.yaml for %s\n", name)
		return nil
	},
}

var deployCmd = &cobra.Command{
	Use:   "deploy [directory]",
	Short: "Build and deploy an application (zero-downtime)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}

		projectRoot, err := config.FindProjectRoot(dir)
		if err != nil {
			abs, _ := filepath.Abs(dir)
			projectRoot = abs
		}

		envFlag, _ := cmd.Flags().GetString("env")

		cfg, err := config.LoadForEnvironment(projectRoot, envFlag)
		if err != nil {
			cfg = &types.AppConfig{
				Name:        filepath.Base(projectRoot),
				Environment: envFlag,
				Serverless: types.ServerlessConfig{
					Enabled:     true,
					IdleTimeout: 5 * time.Minute,
				},
			}
		}

		if len(cfg.Secrets) > 0 {
			sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
			if secErr == nil {
				for k, v := range cfg.Secrets {
					if err := sm.Set(cfg.Name, k, v); err != nil {
						log.Printf("[tengiz] warning: failed to store secret %s: %v", k, err)
					}
				}
				cfg.SecretKeys = make([]string, 0, len(cfg.Secrets))
				for k := range cfg.Secrets {
					cfg.SecretKeys = append(cfg.SecretKeys, k)
				}

				store := config.NewStoreWithEnv(dataDir, envFlag)
				app, _ := store.GetApp(cfg.Name)
				if app != nil {
					app.Config.SecretKeys = cfg.SecretKeys
					store.UpdateApp(*app)
				}
			}
			cfg.Secrets = nil
		}

		fmt.Printf("[tengiz] deploying %s from %s\n", cfg.Name, projectRoot)

		detection, err := builder.Detect(projectRoot)
		if err != nil {
			return fmt.Errorf("detect: %w", err)
		}
		fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)

		if cfg.Port == 0 {
			cfg.Port = detection.InternalPort
		}

		if cfg.Build.Builder == "nixpacks" {
			detection.Framework = builder.FrameworkNixpacks
		}

		deploymentID := fmt.Sprintf("%d", time.Now().Unix())

		b := builder.New(dataDir)
		if cfg.Build.NixpacksConfig != nil {
			b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
		}

		smBuild, secBuildErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
		if secBuildErr == nil {
			appSecrets, listErr := smBuild.GetAllForApp(cfg.Name)
			if listErr == nil && len(appSecrets) > 0 {
				b.SetBuildSecrets(appSecrets)
			}
		}

		store := config.NewStoreWithEnv(dataDir, envFlag)
		imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
		if err != nil {
			fmt.Fprint(os.Stderr, buildLog)
			return fmt.Errorf("build: %w", err)
		}
		fmt.Printf("[tengiz] built image: %s\n", imageTag)

		if buildLog != "" {
			if saveErr := store.SaveBuildLog(cfg.Name, deploymentID, buildLog); saveErr != nil {
				log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
			}
			if pruneErr := store.PruneBuildLogs(cfg.Name, 5); pruneErr != nil {
				log.Printf("[tengiz] warning: failed to prune build logs: %v", pruneErr)
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		// Set up notification manager
		notifyMgr := notify.NewManager(dataDir, envFlag)
		if loadErr := notifyMgr.LoadConfig(); loadErr == nil {
			cfg := notifyMgr.GetConfig()
			if cfg != nil && cfg.Enabled {
				if cfg.Discord != nil {
					notifyMgr.AddNotifier(notify.NewDiscordNotifier(*cfg.Discord))
				}
				if cfg.Slack != nil {
					notifyMgr.AddNotifier(notify.NewSlackNotifier(*cfg.Slack))
				}
				if cfg.Email != nil {
					notifyMgr.AddNotifier(notify.NewEmailNotifier(*cfg.Email))
				}
			}
		}

		// Check if this app already exists (previous deploy)
		existingApp, lookupErr := store.GetApp(cfg.Name)

		if lookupErr != nil {
			// First deploy — simple: allocate port, create container
			port, err := store.AllocatePort(cfg.Name)
			if err != nil {
				notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
					Type:    types.EventDeployFailure,
					AppName: cfg.Name,
					Message: fmt.Sprintf("Port allocation failed for %s: %v", cfg.Name, err),
					Metadata: map[string]string{"environment": envFlag},
				})
				return fmt.Errorf("port: %w", err)
			}

			sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
			if secErr == nil {
				appSecrets, listErr := sm.GetAllForApp(cfg.Name)
				if listErr == nil && len(appSecrets) > 0 {
					if cfg.Env == nil {
						cfg.Env = make(map[string]string, len(appSecrets))
					}
					for k, v := range appSecrets {
						cfg.Env[k] = v
					}
					cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
				}
			}

			if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
				notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
					Type:    types.EventDeployFailure,
					AppName: cfg.Name,
					Message: fmt.Sprintf("Container create failed for %s: %v", cfg.Name, err),
					Metadata: map[string]string{"environment": envFlag},
				})
				return fmt.Errorf("create: %w", err)
			}
			fmt.Printf("[tengiz] running on port %d\n", port)

			store.SaveApp(types.AppEntry{
				Name:     cfg.Name,
				ImageTag: imageTag,
				Port:     port,
				Domains:  cfg.Domains,
				Config:   *cfg,
			})

			store.AddDeployment(cfg.Name, types.DeploymentEntry{
				ID:        deploymentID,
				ImageTag:  imageTag,
				Port:      port,
				CreatedAt: time.Now(),
				Status:    string(types.DeployActive),
			})

			if err := rt.KeepLastNImages(context.Background(), cfg.Name, 5); err != nil {
				log.Printf("[tengiz] warning: image cleanup: %v", err)
			}

			if err := proxy.RegisterRouteWithProxy(cfg.Name, port); err != nil {
				log.Printf("[tengiz] proxy not available (route will be registered on proxy start): %v", err)
			}

			fmt.Printf("[tengiz] deployed: %s at http://%s.tengiz.local:%d\n",
				cfg.Name, cfg.Name, port)

			notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
				Type:    types.EventDeploySuccess,
				AppName: cfg.Name,
				Message: fmt.Sprintf("Deployed %s successfully on port %d", cfg.Name, port),
				Metadata: map[string]string{
					"environment": envFlag,
					"image":       imageTag,
				},
			})
			return nil
		}

		// Zero-downtime deploy: blue/green

		// Allocate a second port for the new container
		newPort, err := store.AllocatePort(cfg.Name)
		if err != nil {
			notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
				Type:    types.EventDeployFailure,
				AppName: cfg.Name,
				Message: fmt.Sprintf("Port allocation failed for %s: %v", cfg.Name, err),
				Metadata: map[string]string{"environment": envFlag},
			})
			return fmt.Errorf("port allocation: %w", err)
		}

		sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
		if secErr == nil {
			appSecrets, listErr := sm.GetAllForApp(cfg.Name)
			if listErr == nil && len(appSecrets) > 0 {
				if cfg.Env == nil {
					cfg.Env = make(map[string]string, len(appSecrets))
				}
				for k, v := range appSecrets {
					cfg.Env[k] = v
				}
				cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
			}
		}

		// Create new container with versioned name
		if err := rt.CreateVersioned(context.Background(), cfg, imageTag, newPort, deploymentID); err != nil {
			store.FreePort(newPort)
			notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
				Type:    types.EventDeployFailure,
				AppName: cfg.Name,
				Message: fmt.Sprintf("Container create failed for %s: %v", cfg.Name, err),
				Metadata: map[string]string{"environment": envFlag},
			})
			return fmt.Errorf("create versioned: %w", err)
		}
		fmt.Printf("[tengiz] new container starting on port %d\n", newPort)

		// Wait for the new container to be ready
		containerName := runtime.ContainerName(cfg.Name, cfg.Environment)
		if err := rt.WaitForReady(context.Background(), fmt.Sprintf("%s-%s", containerName, deploymentID), cfg.Port); err != nil {
			log.Printf("[tengiz] warning: new container may not be ready: %v", err)
		}

		// Register new route with proxy (if running)
		if err := proxy.RegisterRouteWithProxy(cfg.Name, newPort); err != nil {
			log.Printf("[tengiz] proxy not available (route will be registered on proxy start): %v", err)
		}

		// Stop old container
		oldSuffix := existingApp.DeploymentSuffix
		if oldSuffix != "" {
			if err := rt.RemoveBySuffix(context.Background(), containerName, oldSuffix); err != nil {
				log.Printf("[tengiz] warning: failed to remove old container: %v", err)
			}
		} else {
			if err := rt.Remove(context.Background(), containerName); err != nil {
				log.Printf("[tengiz] warning: failed to remove old container: %v", err)
			}
		}

		// Free old port
		store.FreePort(existingApp.Port)

		// Record deployment in history
		store.AddDeployment(cfg.Name, types.DeploymentEntry{
			ID:        deploymentID,
			ImageTag:  imageTag,
			Port:      newPort,
			CreatedAt: time.Now(),
			Status:    string(types.DeployActive),
		})

		// Mark previous deployment as previous
		if existingApp.DeploymentSuffix != "" {
			store.AddDeployment(cfg.Name, types.DeploymentEntry{
				ID:        existingApp.DeploymentSuffix,
				ImageTag:  existingApp.ImageTag,
				Port:      existingApp.Port,
				CreatedAt: time.Now(),
				Status:    string(types.DeployPrevious),
			})
		}

		// Update store with new app entry
		store.SaveApp(types.AppEntry{
			Name:             cfg.Name,
			ImageTag:         imageTag,
			Port:             newPort,
			Domains:          cfg.Domains,
			Config:           *cfg,
			DeploymentSuffix: deploymentID,
		})

		if err := rt.KeepLastNImages(context.Background(), cfg.Name, 5); err != nil {
			log.Printf("[tengiz] warning: image cleanup: %v", err)
		}

		fmt.Printf("[tengiz] deployed (zero-downtime): %s at http://%s.tengiz.local:%d\n",
			cfg.Name, cfg.Name, newPort)

		notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
			Type:    types.EventDeploySuccess,
			AppName: cfg.Name,
			Message: fmt.Sprintf("Deployed %s successfully on port %d (zero-downtime)", cfg.Name, newPort),
			Metadata: map[string]string{
				"environment": envFlag,
				"image":       imageTag,
			},
		})
		return nil
	},
}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		appFlag, _ := cmd.Flags().GetString("app")
		portFlag, _ := cmd.Flags().GetInt("port")
		env, _ := cmd.Flags().GetString("env")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		p := proxy.NewWithEnv(rt, portFlag, env)

		if appFlag != "" {
			p.SetDefaultApp(appFlag)
		}

		idleMgr := idle.NewWithEnv(rt, 5*time.Minute, env)
		p.SetIdleManager(idleMgr)

		store := config.NewStoreWithEnv(dataDir, env)

		healthChecker := health.NewWithEnv(rt, store, env)
		defer healthChecker.StopAll()

		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				p.Register(app.Name, app.Port)
				fmt.Printf("[tengiz] route: %s -> :%d\n", app.Name, app.Port)
				// Register custom domains
				for _, domain := range app.Domains {
					p.RegisterDomain(domain, app.Name)
					fmt.Printf("[tengiz] domain: %s -> %s\n", domain, app.Name)
				}
				healthChecker.Start(app.Name)
			}
		}

		// Register preview deployment routes
		previews, listErr := store.ListAllPreviews()
		if listErr == nil {
			for _, pv := range previews {
				routeKey := fmt.Sprintf("pr-%d.%s", pv.PRNumber, pv.AppName)
				p.Register(routeKey, pv.Port)
				fmt.Printf("[tengiz] preview route: %s -> :%d\n", routeKey, pv.Port)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		go func() {
			<-sig
			cancel()
		}()

		return p.Start(ctx)
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List deployed applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		apps, err := rt.List(context.Background())
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(apps) == 0 {
			fmt.Println("No applications deployed.")
			return nil
		}

		store := config.NewStoreWithEnv(dataDir, env)
		storeApps, _ := store.ListApps()
		envMap := make(map[string]string, len(storeApps))
		healthMap := make(map[string]string, len(storeApps))
		for _, sa := range storeApps {
			envMap[sa.Name] = sa.Config.Environment
			healthMap[sa.Name] = sa.HealthStatus
			if healthMap[sa.Name] == "" {
				healthMap[sa.Name] = string(types.HealthUnknown)
			}
		}

		fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "NAME", "STATE", "PORT", "ENVIRONMENT", "HEALTH")
		for _, a := range apps {
			portStr := fmt.Sprintf("%d", a.Port)
			if a.Port == 0 {
				portStr = "-"
			}
			health := healthMap[a.Name]
			if health == "" {
				health = string(types.HealthUnknown)
			}
			env := envMap[a.Name]
			if env == "" {
				env = "-"
			}
			fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", a.Name, a.State, portStr, env, health)
		}
		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop <app>",
	Short: "Stop an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Stop(context.Background(), runtime.ContainerName(args[0], env))
	},
}

var startCmd = &cobra.Command{
	Use:   "start <app>",
	Short: "Start a stopped application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Start(context.Background(), runtime.ContainerName(args[0], env))
	},
}

var rmCmd = &cobra.Command{
	Use:   "rm <app>",
	Short: "Remove an application completely",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		containerName := runtime.ContainerName(appName, env)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		store := config.NewStoreWithEnv(dataDir, env)
		if err := rt.Remove(context.Background(), containerName); err != nil {
			return err
		}
		store.RemoveApp(appName)

		sm, secErr := getSecretManager(cmd, dataDir, env)
		if secErr == nil {
			secretsList, listErr := sm.List(appName)
			if listErr == nil {
				for k := range secretsList {
					sm.Unset(appName, k)
				}
			}
		}

		fmt.Printf("[tengiz] removed: %s\n", appName)
		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs <app>",
	Short: "Show application logs",
	Long:  "Show application logs. Use -f to follow. Supports --tail, --since, --until, and --grep for filtering.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		follow, _ := cmd.Flags().GetBool("follow")
		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		tail, _ := cmd.Flags().GetInt("tail")
		grep, _ := cmd.Flags().GetString("grep")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		opts := runtime.LogOptions{
			Follow: follow,
			Since:  since,
			Until:  until,
			Tail:   tail,
			Grep:   grep,
		}
		reader, err := rt.Logs(context.Background(), runtime.ContainerName(args[0], env), opts)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = io.Copy(os.Stdout, reader)
		return err
	},
}

var devCmd = &cobra.Command{
	Use:   "dev [directory]",
	Short: "Run the development server locally",
	Long:  "Detects the framework and runs the development server without Docker. Streams output to terminal.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}

		detection, err := builder.Detect(dir)
		if err != nil {
			return fmt.Errorf("detect: %w", err)
		}
		fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)

		var devArgs []string
		switch detection.Framework {
		case builder.FrameworkNextJS, builder.FrameworkVite, builder.FrameworkNode:
			devArgs = []string{"npm", "run", "dev"}
		case builder.FrameworkGo:
			devArgs = []string{"go", "run", "."}
		case builder.FrameworkPython:
			devArgs = []string{"python", "app.py"}
		case builder.FrameworkDocker:
			return fmt.Errorf("dev mode not supported for Docker-based projects; run your Dockerfile directly")
		case builder.FrameworkStatic:
			return fmt.Errorf("dev mode not supported for static sites; serve the directory with any HTTP server")
		default:
			return fmt.Errorf("unsupported framework: %s", detection.Framework)
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		dcmd := exec.CommandContext(ctx, devArgs[0], devArgs[1:]...)
		dcmd.Dir = dir
		dcmd.Stdout = os.Stdout
		dcmd.Stderr = os.Stderr
		dcmd.Env = append(os.Environ(),
			fmt.Sprintf("PORT=%d", detection.InternalPort),
		)

		fmt.Printf("[tengiz] running: %s (http://localhost:%d)\n", detection.Framework, detection.InternalPort)
		if err := dcmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("dev server: %w", err)
		}
		return nil
	},
}

var healthCmd = &cobra.Command{
	Use:   "health <app>",
	Short: "Check application health status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		store := config.NewStoreWithEnv(dataDir, env)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found", appName)
		}

		if app.Config.HealthCheck == nil || !app.Config.HealthCheck.Enabled {
			return fmt.Errorf("health check not configured for %q", appName)
		}

		c := health.New(rt, store)
		if err := c.CheckOnce(cmd.Context(), appName); err != nil {
			fmt.Printf("[tengiz] %s is UNHEALTHY: %v\n", appName, err)
			return nil
		}
		fmt.Printf("[tengiz] %s is healthy\n", appName)
		return nil
	},
}

var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Manage custom domains for applications",
}

var domainAddCmd = &cobra.Command{
	Use:   "add <app> <domain>",
	Short: "Add a custom domain to an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, domain := args[0], args[1]
		store := config.NewStoreWithEnv(dataDir, env)

		if _, err := store.GetApp(appName); err != nil {
			return fmt.Errorf("app %q not found", appName)
		}

		if err := store.AddDomain(appName, domain); err != nil {
			return err
		}

		// Notify proxy if running
		if err := proxy.RegisterDomainWithProxy(domain, appName); err != nil {
			fmt.Printf("[tengiz] domain added to store, but proxy not running: %v\n", err)
		} else {
			fmt.Printf("[tengiz] domain added: %s -> %s\n", domain, appName)
		}
		return nil
	},
}

var domainRemoveCmd = &cobra.Command{
	Use:   "remove <app> <domain>",
	Short: "Remove a custom domain from an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, domain := args[0], args[1]
		store := config.NewStoreWithEnv(dataDir, env)

		if err := store.RemoveDomain(appName, domain); err != nil {
			return err
		}

		// Notify proxy if running
		if err := proxy.UnregisterDomainWithProxy(domain); err != nil {
			fmt.Printf("[tengiz] domain removed from store, but proxy not running: %v\n", err)
		} else {
			fmt.Printf("[tengiz] domain removed: %s from %s\n", domain, appName)
		}
		return nil
	},
}

var domainListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List custom domains for an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		store := config.NewStoreWithEnv(dataDir, env)

		domains, err := store.ListDomains(appName)
		if err != nil {
			return err
		}
		if len(domains) == 0 {
			fmt.Printf("No custom domains for %s.\n", appName)
			return nil
		}
		for _, d := range domains {
			fmt.Println(d)
		}
		return nil
	},
}

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent storage volumes",
}

var volumeAddCmd = &cobra.Command{
	Use:   "add <app> <host_path>:<container_path>",
	Short: "Mount a volume to an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		mount := args[1]
		parts := strings.SplitN(mount, ":", 3)
		if len(parts) < 2 {
			return fmt.Errorf("invalid mount format: use host_path:container_path[:ro]")
		}
		hostPath := parts[0]
		containerPath := parts[1]
		readOnly := len(parts) == 3 && parts[2] == "ro"

		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		vol := types.VolumeConfig{
			HostPath:      hostPath,
			ContainerPath: containerPath,
			ReadOnly:      readOnly,
		}
		if err := store.AddVolume(appName, vol); err != nil {
			return err
		}
		fmt.Printf("[tengiz] mounted %s:%s for %s\n", hostPath, containerPath, appName)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <app> <host_path>",
	Short: "Unmount a volume from an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		if err := store.RemoveVolume(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] removed volume %s from %s\n", args[1], args[0])
		return nil
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List mounted volumes for an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		vols, err := store.ListVolumes(args[0])
		if err != nil {
			return err
		}
		if len(vols) == 0 {
			fmt.Printf("No volumes mounted for %s.\n", args[0])
			return nil
		}
		fmt.Printf("Volumes for %s:\n", args[0])
		for _, v := range vols {
			ro := ""
			if v.ReadOnly {
				ro = " (read-only)"
			}
			fmt.Printf("  %s:%s%s\n", v.HostPath, v.ContainerPath, ro)
		}
		return nil
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <app>",
	Short: "Rollback to the previous deployment",
	Long:  "Reverses the most recent deployment. The previous active container is started and the current one is stopped.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		store := config.NewStoreWithEnv(dataDir, env)

		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		prevDep, err := store.GetPreviousDeployment(appName)
		if err != nil {
			return fmt.Errorf("no previous deployment found: %w", err)
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		containerName := runtime.ContainerName(appName, env)

		newPort, err := store.AllocatePort(appName)
		if err != nil {
			return fmt.Errorf("port allocation: %w", err)
		}

		if err := rt.CreateFromImage(cmd.Context(), &app.Config, prevDep.ImageTag, newPort); err != nil {
			store.FreePort(newPort)
			return fmt.Errorf("create rollback container: %w", err)
		}

		if err := rt.WaitForReady(cmd.Context(), containerName, app.Config.Port); err != nil {
			log.Printf("[tengiz] warning: rollback container may not be ready: %v", err)
		}

		if err := proxy.RegisterRouteWithProxy(appName, newPort); err != nil {
			log.Printf("[tengiz] proxy not available: %v", err)
		}

		if app.DeploymentSuffix != "" {
			if err := rt.RemoveBySuffix(cmd.Context(), containerName, app.DeploymentSuffix); err != nil {
				log.Printf("[tengiz] warning: failed to remove current container: %v", err)
			}
		} else {
			if err := rt.Remove(cmd.Context(), containerName); err != nil {
				log.Printf("[tengiz] warning: failed to remove current container: %v", err)
			}
		}

		store.FreePort(app.Port)

		if app.DeploymentSuffix != "" {
			store.UpdateDeploymentStatus(appName, app.DeploymentSuffix, string(types.DeployRolled))
		}

		store.UpdateDeploymentStatus(appName, prevDep.ID, string(types.DeployActive))

		store.SaveApp(types.AppEntry{
			Name:             app.Name,
			ImageTag:         prevDep.ImageTag,
			Port:             newPort,
			Domains:          app.Domains,
			Config:           app.Config,
			DeploymentSuffix: prevDep.ID,
		})

		fmt.Printf("[tengiz] rolled back %s to deployment %s (port %d)\n", appName, prevDep.ID, newPort)
		return nil
	},
}

var buildLogsCmd = &cobra.Command{
	Use:   "build-logs <app> [deployment-id]",
	Short: "Show build logs for an application",
	Long: `Show build logs from previous deployments.

Without a deployment ID, lists all available build logs.
With a deployment ID, shows the full build output for that deployment.

Use --tail N to show only the last N lines of the latest build log.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		tailLines, _ := cmd.Flags().GetInt("tail")

		store := config.NewStoreWithEnv(dataDir, env)

		if len(args) == 2 {
			deploymentID := args[1]
			content, err := store.GetBuildLog(appName, deploymentID)
			if err != nil {
				return fmt.Errorf("build log for %s@%s: %w", appName, deploymentID, err)
			}
			if tailLines > 0 {
				lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
				if len(lines) > tailLines {
					lines = lines[len(lines)-tailLines:]
				}
				fmt.Print(strings.Join(lines, "\n"))
				if !strings.HasSuffix(content, "\n") {
					fmt.Println()
				}
			} else {
				fmt.Print(content)
				if !strings.HasSuffix(content, "\n") {
					fmt.Println()
				}
			}
			return nil
		}

		ids, err := store.ListBuildLogs(appName)
		if err != nil {
			return fmt.Errorf("list build logs: %w", err)
		}
		if len(ids) == 0 {
			fmt.Printf("No build logs for %s.\n", appName)
			return nil
		}

		if tailLines > 0 {
			content, err := store.GetBuildLog(appName, ids[0])
			if err != nil {
				return fmt.Errorf("get latest build log: %w", err)
			}
			lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
			if len(lines) > tailLines {
				lines = lines[len(lines)-tailLines:]
			}
			fmt.Print(strings.Join(lines, "\n"))
			if !strings.HasSuffix(content, "\n") {
				fmt.Println()
			}
			return nil
		}

		fmt.Printf("Build logs for %s:\n", appName)
		for _, id := range ids {
			fmt.Printf("  %s\n", id)
		}
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:   "run <app> [--] <command> [args...]",
	Short: "Run a one-off command in a temporary container",
	Long: `Run a one-off command inside a temporary container created from the
app's deployed image. The container is automatically removed on exit.

Useful for database migrations, console access, and data import tasks.

Examples:
  tengiz run myapp -- python manage.py migrate
  tengiz run myapp -- rails console
  tengiz run -i myapp -- bash`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]
		command := args[1:]
		interactive, _ := cmd.Flags().GetBool("interactive")

		store := config.NewStoreWithEnv(dataDir, env)

		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		imageTag := app.ImageTag
		if imageTag == "" {
			return fmt.Errorf("app %q has no image tag — deploy it first", appName)
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Printf("[tengiz] running: %s (%s)\n", strings.Join(command, " "), imageTag)

		extraEnv := make(map[string]string)
		envFlags, _ := cmd.Flags().GetStringArray("env")
		for _, e := range envFlags {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid env format %q, use KEY=VALUE", e)
			}
			extraEnv[parts[0]] = parts[1]
		}

		sm, secErr := getSecretManager(cmd, dataDir, env)
		if secErr == nil {
			appSecrets, listErr := sm.GetAllForApp(appName)
			if listErr == nil && len(appSecrets) > 0 {
				for k, v := range appSecrets {
					extraEnv[k] = v
				}
				extraEnv = secrets.ResolveInterpolations(extraEnv, appSecrets)
			}
		}

		opts := runtime.RunOptions{
			Interactive: interactive,
			ExtraEnv:    extraEnv,
		}

		if err := rt.Run(cmd.Context(), &app.Config, imageTag, command, opts); err != nil {
			return fmt.Errorf("run: %w", err)
		}

		return nil
	},
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker resources (containers, images, networks, volumes).

Tengiz-managed containers are always protected: pruning uses label-based
filtering (label!=tengiz-app) so running and stopped app containers — including
scale-to-zero cold-start state — are never removed.

By default (no flags) removes:
  - stopped containers not managed by Tengiz
  - dangling (unreferenced) images
  - unused networks

If any category flag is set, only those categories are cleaned.
Use --all to clean everything, including unused volumes (data loss risk).

Examples:
  tengiz cleanup
  tengiz cleanup --all-images
  tengiz cleanup --volumes
  tengiz cleanup --all`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Println("[tengiz] cleaning up unused Docker resources...")
		res, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Printf("[tengiz] cleanup complete:\n")
		fmt.Printf("  containers removed: %d\n", res.ContainersDeleted)
		fmt.Printf("  images removed: %d\n", res.ImagesDeleted)
		fmt.Printf("  networks removed: %d\n", res.NetworksDeleted)
		fmt.Printf("  volumes removed: %d\n", res.VolumesDeleted)
		fmt.Printf("  space reclaimed: %s\n", humanizeBytes(res.TotalReclaimedBytes))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	allImages, _ := cmd.Flags().GetBool("all-images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")

	if all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			AllImages:  true,
			Networks:   true,
			Volumes:    true,
		}, nil
	}
	if !containers && !images && !allImages && !networks && !volumes {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
		}, nil
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images || allImages,
		AllImages:  allImages,
		Networks:   networks,
		Volumes:    volumes,
	}, nil
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

var gitCmd = &cobra.Command{
	Use:   "git",
	Short: "Manage git deployment configuration",
}

var gitConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Generate SSH deploy key for git auto-deploy",
	Long:  "Generates an Ed25519 SSH key pair stored in ~/.tengiz/ssh/. Prints the public key — add it to your git provider as a deploy key.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if git.HasKey(dataDir) {
			fmt.Println("[tengiz] SSH key already exists. Use 'git disconnect' to remove it first.")
			return nil
		}

		pub, err := git.GenerateKey(dataDir)
		if err != nil {
			return fmt.Errorf("generate key: %w", err)
		}

		fmt.Println("[tengiz] SSH deploy key generated!")
		fmt.Println()
		fmt.Println("Add this public key to your git provider (GitHub > Settings > Deploy Keys):")
		fmt.Println()
		fmt.Println(pub)
		fmt.Println()
		fmt.Println("Or on GitHub: repo > Settings > Deploy Keys > Add deploy key")
		fmt.Println("On GitLab:   repo > Settings > Repository > Deploy Keys")
		return nil
	},
}

var gitDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Remove SSH deploy key for git auto-deploy",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !git.HasKey(dataDir) {
			fmt.Println("[tengiz] No SSH key found.")
			return nil
		}
		if err := git.RemoveKey(dataDir); err != nil {
			return fmt.Errorf("remove key: %w", err)
		}
		fmt.Println("[tengiz] SSH key removed.")
		return nil
	},
}

var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Start the git webhook server for auto-deploy",
	Long:  "Starts an HTTP server that listens for GitHub/GitLab/Bitbucket/Gitea push events and triggers automatic deployment.",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		env, _ := cmd.Flags().GetString("env")
		configPath, _ := cmd.Flags().GetString("config")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		pipeline := gitdeploy.NewPipelineWithEnv(dataDir, env, rt, store)

		deployFn := webhook.DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
			return pipeline.Deploy(ctx, repo, branch, provider)
		})

		previewMgr := preview.NewManager(dataDir, store, rt)

		previewFn := webhook.PreviewFunc(func(appName string, prNumber int, branch, repoURL string) error {
			ctx := context.Background()
			if branch == "" {
				return previewMgr.Delete(ctx, appName, prNumber)
			}
			existing, err := store.GetPreview(appName, prNumber)
			if existing != nil && err == nil {
				_, updateErr := previewMgr.Update(ctx, appName, prNumber, branch)
				return updateErr
			}
			_, createErr := previewMgr.Create(ctx, appName, prNumber, branch, repoURL)
			return createErr
		})

		// Load webhook config from .tengiz.yaml
		var whCfg *webhook.Config
		if configPath != "" {
			twc, loadErr := config.LoadWebhookConfig(configPath)
			if loadErr != nil {
				return fmt.Errorf("webhook config: %w", loadErr)
			}
			if twc != nil {
				whCfg = &webhook.Config{
					Secret:          twc.Secret,
					AllowedBranches: twc.AllowedBranches,
					Port:            twc.Port,
				}
			}
		}

		// Config port overrides CLI flag if set
		if whCfg != nil && whCfg.Port > 0 {
			port = whCfg.Port
		}

		s := webhook.New(dataDir, whCfg, deployFn)
		s.SetPreviewFunc(previewFn)
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		fmt.Printf("[tengiz] starting webhook server on :%d\n", port)
		return s.Start(ctx, port)
	},
}

var notificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Manage notification channels",
}

var notificationEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable notifications",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		mgr := notify.NewManager(dataDir, env)
		if err := mgr.LoadConfig(); err != nil {
			return err
		}
		cfg := mgr.GetConfig()
		if cfg == nil {
			cfg = &types.NotificationConfig{Enabled: true}
		} else {
			cfg.Enabled = true
		}
		if err := mgr.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Println("[tengiz] notifications enabled")
		return nil
	},
}

var notificationDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable notifications",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		mgr := notify.NewManager(dataDir, env)
		if err := mgr.LoadConfig(); err != nil {
			return err
		}
		cfg := mgr.GetConfig()
		if cfg == nil {
			cfg = &types.NotificationConfig{Enabled: false}
		} else {
			cfg.Enabled = false
		}
		if err := mgr.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Println("[tengiz] notifications disabled")
		return nil
	},
}

var notificationConfigCmd = &cobra.Command{
	Use:   "config <app>",
	Short: "Configure which events trigger notifications",
	Long: `Set which events trigger notifications. Events: deploy:success, deploy:failure, health:alert, container:stop, system:warning.
Use --events flag (comma-separated) or --all to enable all events.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]

		allEvents, _ := cmd.Flags().GetBool("all")
		eventsStr, _ := cmd.Flags().GetString("events")

		mgr := notify.NewManager(dataDir, env)
		if err := mgr.LoadConfig(); err != nil {
			return err
		}

		cfg := mgr.GetConfig()
		if cfg == nil {
			cfg = &types.NotificationConfig{}
		}

		if allEvents {
			cfg.Events = []types.NotificationEventType{
				types.EventDeploySuccess,
				types.EventDeployFailure,
				types.EventHealthAlert,
				types.EventContainerStop,
				types.EventSystemWarning,
			}
		} else if eventsStr != "" {
			parts := strings.Split(eventsStr, ",")
			events := make([]types.NotificationEventType, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				events = append(events, types.NotificationEventType(p))
			}
			cfg.Events = events
		}

		if err := mgr.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("[tengiz] notification events configured for %s\n", appName)
		return nil
	},
}

var notificationSetChannelCmd = &cobra.Command{
	Use:   "set-channel <type>",
	Short: "Configure a notification channel",
	Long: `Configure a notification channel. Types: discord, slack, email.

Discord: --webhook-url <url>
Slack:   --webhook-url <url>
Email:   --smtp-server <host> --smtp-port <port> --from <addr> --to <addr> [--username <user> --password <pass>] [--tls]`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		channelType := args[0]

		mgr := notify.NewManager(dataDir, env)
		if err := mgr.LoadConfig(); err != nil {
			return err
		}

		cfg := mgr.GetConfig()
		if cfg == nil {
			cfg = &types.NotificationConfig{}
		}

		switch types.ChannelType(channelType) {
		case types.ChannelDiscord:
			webhookURL, _ := cmd.Flags().GetString("webhook-url")
			if webhookURL == "" {
				return fmt.Errorf("--webhook-url is required for discord")
			}
			cfg.Discord = &types.DiscordConfig{WebhookURL: webhookURL}
		case types.ChannelSlack:
			webhookURL, _ := cmd.Flags().GetString("webhook-url")
			if webhookURL == "" {
				return fmt.Errorf("--webhook-url is required for slack")
			}
			cfg.Slack = &types.SlackConfig{WebhookURL: webhookURL}
		case types.ChannelEmail:
			smtpServer, _ := cmd.Flags().GetString("smtp-server")
			smtpPort, _ := cmd.Flags().GetInt("smtp-port")
			from, _ := cmd.Flags().GetString("from")
			to, _ := cmd.Flags().GetString("to")
			username, _ := cmd.Flags().GetString("username")
			password, _ := cmd.Flags().GetString("password")
			useTLS, _ := cmd.Flags().GetBool("tls")

			if smtpServer == "" || from == "" || to == "" {
				return fmt.Errorf("--smtp-server, --from, and --to are required for email")
			}
			if smtpPort == 0 {
				smtpPort = 587
			}
			cfg.Email = &types.EmailConfig{
				SMTPServer: smtpServer,
				SMTPPort:   smtpPort,
				Username:   username,
				Password:   password,
				From:       from,
				To:         to,
				UseTLS:     useTLS,
			}
		default:
			return fmt.Errorf("unknown channel type %q; supported: discord, slack, email", channelType)
		}

		if err := mgr.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("[tengiz] notification channel %s configured\n", channelType)
		return nil
	},
}

var notificationShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current notification configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		mgr := notify.NewManager(dataDir, env)
		if err := mgr.LoadConfig(); err != nil {
			return err
		}

		cfg := mgr.GetConfig()
		if cfg == nil {
			fmt.Println("Notifications not configured.")
			return nil
		}

		fmt.Printf("Enabled: %v\n", cfg.Enabled)
		fmt.Printf("Events: %v\n", cfg.Events)
		if cfg.Discord != nil {
			fmt.Printf("Discord: configured (webhook: %s)\n", maskSecret(cfg.Discord.WebhookURL))
		}
		if cfg.Slack != nil {
			fmt.Printf("Slack: configured (webhook: %s)\n", maskSecret(cfg.Slack.WebhookURL))
		}
		if cfg.Email != nil {
			fmt.Printf("Email: configured (%s -> %s, server: %s:%d)\n", cfg.Email.From, cfg.Email.To, cfg.Email.SMTPServer, cfg.Email.SMTPPort)
		}
		return nil
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage environment variables for an application",
}

var configSetCmd = &cobra.Command{
	Use:   "set <app> <key> <value>",
	Short: "Set an environment variable",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, key, value := args[0], args[1], args[2]
		store := config.NewStoreWithEnv(dataDir, env)

		if isSecret, _ := cmd.Flags().GetBool("secret"); isSecret {
			sm, err := getSecretManager(cmd, dataDir, env)
			if err != nil {
				return fmt.Errorf("secrets manager: %w", err)
			}
			if err := sm.Set(appName, key, value); err != nil {
				return fmt.Errorf("set secret: %w", err)
			}

			app, _ := store.GetApp(appName)
			if app != nil {
				app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
				store.UpdateApp(*app)
			}

			fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
			return nil
		}

		if err := store.SetEnv(appName, key, value); err != nil {
			return err
		}
		fmt.Printf("[tengiz] set %s=%s for %s\n", key, value, appName)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <app> <key>",
	Short: "Get an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		val, ok, err := store.GetEnv(args[0], args[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("env var %q not set for %s", args[1], args[0])
		}

		sm, secErr := getSecretManager(cmd, dataDir, env)
		if secErr == nil {
			secretKeys, _ := sm.List(args[0])
			if _, isSecret := secretKeys[args[1]]; isSecret {
				val = maskSecret(val)
			}
		}

		fmt.Printf("%s=%s\n", args[1], val)
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <app> <key>",
	Short: "Remove an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		if err := store.UnsetEnv(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] unset %s for %s\n", args[1], args[0])
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show <app>",
	Short: "Show all environment variables for an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		envVars, err := store.ListEnv(args[0])
		if err != nil {
			return err
		}
		if len(envVars) == 0 {
			fmt.Printf("No environment variables set for %s.\n", args[0])
			return nil
		}

		sm, secErr := getSecretManager(cmd, dataDir, env)
		if secErr == nil {
			secretKeys, _ := sm.List(args[0])
			for k := range secretKeys {
				if v, ok := envVars[k]; ok {
					envVars[k] = maskSecret(v)
				}
			}
		}

		for k, v := range envVars {
			fmt.Printf("%s=%s\n", k, v)
		}
		return nil
	},
}

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage encrypted secrets for an application",
}

var secretSetCmd = &cobra.Command{
	Use:   "set <app> <key> <value>",
	Short: "Set an encrypted secret",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, key, value := args[0], args[1], args[2]

		store := config.NewStoreWithEnv(dataDir, env)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		sm, err := getSecretManager(cmd, dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		if err := sm.Set(appName, key, value); err != nil {
			return fmt.Errorf("set secret: %w", err)
		}

		app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
		if err := store.UpdateApp(*app); err != nil {
			return fmt.Errorf("update app: %w", err)
		}

		fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
		return nil
	},
}

var secretGetCmd = &cobra.Command{
	Use:   "get <app> <key>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, key := args[0], args[1]

		sm, err := getSecretManager(cmd, dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		val, ok, err := sm.Get(appName, key)
		if err != nil {
			return fmt.Errorf("get secret: %w", err)
		}
		if !ok {
			return fmt.Errorf("secret %q not found for app %q", key, appName)
		}

		fmt.Printf("%s=%s\n", key, val)
		return nil
	},
}

var secretUnsetCmd = &cobra.Command{
	Use:   "unset <app> <key>",
	Short: "Remove a secret",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, key := args[0], args[1]

		store := config.NewStoreWithEnv(dataDir, env)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		sm, err := getSecretManager(cmd, dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		if err := sm.Unset(appName, key); err != nil {
			return fmt.Errorf("unset secret: %w", err)
		}

		app.Config.SecretKeys = removeFromSlice(app.Config.SecretKeys, key)
		if err := store.UpdateApp(*app); err != nil {
			return fmt.Errorf("update app: %w", err)
		}

		fmt.Printf("[tengiz] secret %s unset for %s\n", key, appName)
		return nil
	},
}

var secretListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List all secrets for an application (values masked)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]

		sm, err := getSecretManager(cmd, dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		secrets, err := sm.List(appName)
		if err != nil {
			return fmt.Errorf("list secrets: %w", err)
		}

		if len(secrets) == 0 {
			fmt.Printf("No secrets for %s.\n", appName)
			return nil
		}

		keys := make([]string, 0, len(secrets))
		for k := range secrets {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			fmt.Printf("%s=****\n", k)
		}
		return nil
	},
}

func addToSlice(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func removeFromSlice(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func getSecretManager(cmd *cobra.Command, dataDir, env string) (*secrets.Manager, error) {
	provider, _ := cmd.Flags().GetString("provider")
	vaultAddr, _ := cmd.Flags().GetString("vault-addr")
	vaultToken, _ := cmd.Flags().GetString("vault-token")
	dopplerToken, _ := cmd.Flags().GetString("doppler-token")
	dopplerProject, _ := cmd.Flags().GetString("doppler-project")
	dopplerConfig, _ := cmd.Flags().GetString("doppler-config")
	return secrets.NewManagerFromConfig(dataDir, env, provider, vaultAddr, vaultToken, dopplerToken, dopplerProject, dopplerConfig)
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:1] + "**" + s[len(s)-1:]
}

func getwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "app"
	}
	return wd
}

func addSecretProviderFlags(cmd *cobra.Command) {
	cmd.Flags().String("provider", "", "secrets provider: local, vault, doppler (default: local)")
	cmd.Flags().String("vault-addr", "", "Vault server address")
	cmd.Flags().String("vault-token", "", "Vault token")
	cmd.Flags().String("doppler-token", "", "Doppler service token")
	cmd.Flags().String("doppler-project", "", "Doppler project")
	cmd.Flags().String("doppler-config", "", "Doppler config")
}

func Execute() {
	proxyCmd.Flags().StringP("app", "a", "", "route all requests to this app (bypasses hostname routing)")
	proxyCmd.Flags().IntP("port", "p", 8080, "proxy listen port")
	proxyCmd.Flags().String("env", "production", "environment for proxy routing")
	buildLogsCmd.Flags().Int("tail", 0, "show only last N lines of the latest build log")
	configSetCmd.Flags().Bool("secret", false, "Store as encrypted secret instead of plaintext env var")
	notificationConfigCmd.Flags().Bool("all", false, "enable notifications for all event types")
	notificationConfigCmd.Flags().String("events", "", "comma-separated list of event types to notify on")
	notificationSetChannelCmd.Flags().String("webhook-url", "", "webhook URL for Discord/Slack")
	notificationSetChannelCmd.Flags().String("smtp-server", "", "SMTP server hostname")
	notificationSetChannelCmd.Flags().Int("smtp-port", 0, "SMTP server port")
	notificationSetChannelCmd.Flags().String("from", "", "sender email address")
	notificationSetChannelCmd.Flags().String("to", "", "recipient email address")
	notificationSetChannelCmd.Flags().String("username", "", "SMTP username")
	notificationSetChannelCmd.Flags().String("password", "", "SMTP password")
	notificationSetChannelCmd.Flags().Bool("tls", false, "use TLS for SMTP")
	addSecretProviderFlags(secretSetCmd)
	addSecretProviderFlags(secretGetCmd)
	addSecretProviderFlags(secretUnsetCmd)
	addSecretProviderFlags(secretListCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
