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
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var dataDir string

func init() {
	home, _ := os.UserHomeDir()
	dataDir = filepath.Join(home, ".tengiz")
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(devCmd)
}

var rootCmd = &cobra.Command{
	Use:   "tengiz",
	Short: "Tengiz - Serverless deployment platform",
	Long:  "Tengiz is a Vercel alternative. Deploy any app with scale-to-zero.",
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

		content := fmt.Sprintf(`name: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# domains:
#   - app.example.com
`, name)

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

		cfg, err := config.Load(projectRoot)
		if err != nil {
			cfg = &types.AppConfig{
				Name: filepath.Base(projectRoot),
				Serverless: types.ServerlessConfig{
					Enabled:     true,
					IdleTimeout: 5 * time.Minute,
				},
			}
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

		b := builder.New(dataDir)
		imageTag, err := b.Build(context.Background(), projectRoot, cfg.Name, detection)
		if err != nil {
			return fmt.Errorf("build: %w", err)
		}
		fmt.Printf("[tengiz] built image: %s\n", imageTag)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStore(dataDir)

		// Check if this app already exists (previous deploy)
		existingApp, lookupErr := store.GetApp(cfg.Name)

		if lookupErr != nil {
			// First deploy — simple: allocate port, create container
			port, err := store.AllocatePort(cfg.Name)
			if err != nil {
				return fmt.Errorf("port: %w", err)
			}

			if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
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

			if err := proxy.RegisterRouteWithProxy(cfg.Name, port); err != nil {
				log.Printf("[tengiz] proxy not available (route will be registered on proxy start): %v", err)
			}

			fmt.Printf("[tengiz] deployed: %s at http://%s.tengiz.local:%d\n",
				cfg.Name, cfg.Name, port)
			return nil
		}

		// Zero-downtime deploy: blue/green
		deploymentID := fmt.Sprintf("%d", time.Now().Unix())

		// Allocate a second port for the new container
		newPort, err := store.AllocatePort(cfg.Name)
		if err != nil {
			return fmt.Errorf("port allocation: %w", err)
		}

		// Create new container with versioned name
		if err := rt.CreateVersioned(context.Background(), cfg, imageTag, newPort, deploymentID); err != nil {
			store.FreePort(newPort)
			return fmt.Errorf("create versioned: %w", err)
		}
		fmt.Printf("[tengiz] new container starting on port %d\n", newPort)

		// Wait for the new container to be ready
		if err := rt.WaitForReady(context.Background(), fmt.Sprintf("%s-%s", cfg.Name, deploymentID), cfg.Port); err != nil {
			log.Printf("[tengiz] warning: new container may not be ready: %v", err)
		}

		// Register new route with proxy (if running)
		if err := proxy.RegisterRouteWithProxy(cfg.Name, newPort); err != nil {
			log.Printf("[tengiz] proxy not available (route will be registered on proxy start): %v", err)
		}

		// Stop old container
		oldSuffix := existingApp.DeploymentSuffix
		if oldSuffix != "" {
			if err := rt.RemoveBySuffix(context.Background(), cfg.Name, oldSuffix); err != nil {
				log.Printf("[tengiz] warning: failed to remove old container: %v", err)
			}
		} else {
			if err := rt.Remove(context.Background(), cfg.Name); err != nil {
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

		fmt.Printf("[tengiz] deployed (zero-downtime): %s at http://%s.tengiz.local:%d\n",
			cfg.Name, cfg.Name, newPort)
		return nil
	},
}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		appFlag, _ := cmd.Flags().GetString("app")
		portFlag, _ := cmd.Flags().GetInt("port")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		p := proxy.New(rt, portFlag)

		if appFlag != "" {
			p.SetDefaultApp(appFlag)
		}

		idleMgr := idle.New(rt, 5*time.Minute)
		p.SetIdleManager(idleMgr)

		store := config.NewStore(dataDir)
		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				p.Register(app.Name, app.Port)
				fmt.Printf("[tengiz] route: %s -> :%d\n", app.Name, app.Port)
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

		fmt.Printf("%-20s %-10s %-8s\n", "NAME", "STATE", "PORT")
		for _, a := range apps {
			portStr := fmt.Sprintf("%d", a.Port)
			if a.Port == 0 {
				portStr = "-"
			}
			fmt.Printf("%-20s %-10s %-8s\n", a.Name, a.State, portStr)
		}
		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop <app>",
	Short: "Stop an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Stop(context.Background(), args[0])
	},
}

var startCmd = &cobra.Command{
	Use:   "start <app>",
	Short: "Start a stopped application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Start(context.Background(), args[0])
	},
}

var rmCmd = &cobra.Command{
	Use:   "rm <app>",
	Short: "Remove an application completely",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		store := config.NewStore(dataDir)
		if err := rt.Remove(context.Background(), args[0]); err != nil {
			return err
		}
		store.RemoveApp(args[0])
		fmt.Printf("[tengiz] removed: %s\n", args[0])
		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs <app>",
	Short: "Show application logs",
	Long:  "Show application logs. Use -f to follow.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		reader, err := rt.Logs(context.Background(), args[0], follow)
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

func getwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "app"
	}
	return wd
}

func Execute() {
	proxyCmd.Flags().StringP("app", "a", "", "route all requests to this app (bypasses hostname routing)")
	proxyCmd.Flags().IntP("port", "p", 8080, "proxy listen port")
	logsCmd.Flags().BoolP("follow", "f", false, "follow log output")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
