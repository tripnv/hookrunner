package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "Config file path (default: ~/.hookrunner/config.yaml)")
	daemon := flag.Bool("daemon", false, "Run as background daemon")
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
		*configPath = defaultConfigPath()
	}

	if *init_ {
		if err := generateDefaultConfig(*configPath); err != nil {
			log.Fatalf("Failed to generate config: %v", err)
		}
		fmt.Printf("Config written to %s\n", *configPath)
		return
	}

	if *stop {
		if err := stopDaemon(*configPath); err != nil {
			log.Fatalf("Failed to stop daemon: %v", err)
		}
		return
	}

	if *status {
		if err := checkDaemonStatus(*configPath); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *port != 0 {
		cfg.Port = *port
	}
	if *noFunnel {
		cfg.Funnel.Enabled = false
	}

	if *daemon {
		if err := daemonize(*configPath, cfg); err != nil {
			log.Fatalf("Failed to daemonize: %v", err)
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var funnel *FunnelProcess
	if cfg.Funnel.Enabled {
		funnel, err = startFunnel(cfg.Port, cfg.Funnel.URL)
		if err != nil {
			log.Printf("WARNING: Tailscale Funnel failed to start: %v", err)
			log.Printf("Server will still start on 127.0.0.1:%d", cfg.Port)
		} else {
			log.Printf("Tailscale Funnel started for port %d", cfg.Port)
		}
	}

	srv := newServer(cfg)
	go func() {
		if err := srv.start(); err != nil {
			log.Printf("Server error: %v", err)
			cancel()
		}
	}()

	log.Printf("hookrunner listening on 127.0.0.1:%d", cfg.Port)

	// Write PID file if running as daemon child
	pidFile := expandTilde(cfg.Daemon.PIDFile)
	if err := writePIDFile(pidFile); err != nil {
		log.Printf("WARNING: Failed to write PID file: %v", err)
	}
	defer os.Remove(pidFile)

	select {
	case <-sigCh:
		log.Println("Shutting down...")
	case <-ctx.Done():
	}

	srv.shutdown()

	if funnel != nil {
		stopFunnel(funnel)
	}

	log.Println("hookrunner stopped")
}
