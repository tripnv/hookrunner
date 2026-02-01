package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"hookrunner/internal/config"
	"hookrunner/internal/daemon"
	"hookrunner/internal/funnel"
	"hookrunner/internal/server"
	"hookrunner/internal/webhook"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "Config file path (default: ~/.hookrunner/config.yaml)")
	daemonFlag := flag.Bool("daemon", false, "Run as background daemon")
	stop := flag.Bool("stop", false, "Stop running daemon")
	status := flag.Bool("status", false, "Check daemon status")
	port := flag.Int("port", 0, "Override config port")
	noFunnel := flag.Bool("no-funnel", false, "Disable Tailscale Funnel")
	init_ := flag.Bool("init", false, "Generate default config")
	ver := flag.Bool("version", false, "Print version")
	flag.Parse()

	if *ver {
		fmt.Printf("hookrunner %s\n", version)
		return
	}

	if *configPath == "" {
		*configPath = config.DefaultPath()
	}

	if *init_ {
		if err := config.GenerateDefault(*configPath); err != nil {
			log.Fatalf("Failed to generate config: %v", err)
		}
		fmt.Printf("Config written to %s\n", *configPath)
		return
	}

	if *stop {
		if err := daemon.Stop(*configPath); err != nil {
			log.Fatalf("Failed to stop daemon: %v", err)
		}
		return
	}

	if *status {
		if err := daemon.Status(*configPath); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *port != 0 {
		cfg.Port = *port
	}
	if *noFunnel {
		cfg.Funnel.Enabled = false
	}

	if *daemonFlag {
		if err := daemon.Daemonize(*configPath, cfg); err != nil {
			log.Fatalf("Failed to daemonize: %v", err)
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var fp *funnel.Process
	if cfg.Funnel.Enabled {
		fp, err = funnel.Start(cfg.Port, cfg.Funnel.URL)
		if err != nil {
			log.Printf("WARNING: Tailscale Funnel failed to start: %v", err)
			log.Printf("Server will still start on 127.0.0.1:%d", cfg.Port)
		} else {
			log.Printf("Tailscale Funnel started for port %d", cfg.Port)
		}
	}

	webhookHandler := webhook.Handler(cfg)
	srv := server.New(cfg.Port, webhookHandler)
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
			cancel()
		}
	}()

	log.Printf("hookrunner listening on 127.0.0.1:%d", cfg.Port)

	// Write PID file if running as daemon child
	pidFile := config.ExpandTilde(cfg.Daemon.PIDFile)
	if err := daemon.WritePIDFile(pidFile); err != nil {
		log.Printf("WARNING: Failed to write PID file: %v", err)
	}
	defer os.Remove(pidFile)

	select {
	case <-sigCh:
		log.Println("Shutting down...")
	case <-ctx.Done():
	}

	srv.Shutdown()

	if fp != nil {
		funnel.Stop(fp)
	}

	log.Println("hookrunner stopped")
}
