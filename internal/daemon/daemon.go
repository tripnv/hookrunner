package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hookrunner/internal/config"
)

func Daemonize(configPath string, cfg *config.Config) error {
	logPath := config.ExpandTilde(cfg.Daemon.LogFile)
	logDir := logPath[:strings.LastIndex(logPath, "/")]
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	// Re-exec without --daemon
	args := []string{"--config", configPath}
	for _, arg := range os.Args[1:] {
		if arg == "--daemon" || arg == "-daemon" {
			continue
		}
		// Skip if already added via --config
		if arg == "--config" || arg == "-config" {
			continue
		}
		args = append(args, arg)
	}

	exe, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("finding executable: %w", err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting daemon: %w", err)
	}

	fmt.Printf("hookrunner started as daemon (PID %d)\n", cmd.Process.Pid)
	fmt.Printf("Log file: %s\n", logPath)

	// Detach — don't wait for child
	cmd.Process.Release()
	logFile.Close()

	return nil
}

func WritePIDFile(path string) error {
	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600)
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}
	return pid, nil
}

func Stop(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		// Try default PID path
		cfg = &config.Config{}
		config.ApplyDefaults(cfg)
	}

	pidPath := config.ExpandTilde(cfg.Daemon.PIDFile)
	pid, err := readPIDFile(pidPath)
	if err != nil {
		return fmt.Errorf("no running daemon found (PID file: %s): %w", pidPath, err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("process %d not found", pid)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("sending SIGTERM to %d: %w", pid, err)
	}

	fmt.Printf("Sent SIGTERM to hookrunner (PID %d)\n", pid)

	// Wait for process to exit
	for i := 0; i < 50; i++ {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			fmt.Println("hookrunner stopped")
			os.Remove(pidPath)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("Process did not stop within 5 seconds, sending SIGKILL")
	proc.Signal(syscall.SIGKILL)
	os.Remove(pidPath)
	return nil
}

func Status(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = &config.Config{}
		config.ApplyDefaults(cfg)
	}

	pidPath := config.ExpandTilde(cfg.Daemon.PIDFile)
	pid, err := readPIDFile(pidPath)
	if err != nil {
		return fmt.Errorf("hookrunner is not running (no PID file at %s)", pidPath)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("hookrunner is not running (PID %d not found)", pid)
	}

	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("hookrunner is not running (PID %d is stale)", pid)
	}

	fmt.Printf("hookrunner is running (PID %d)\n", pid)
	return nil
}
