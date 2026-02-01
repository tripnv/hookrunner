package funnel

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Process struct {
	cmd *exec.Cmd
}

func Start(port int, urlOverride string) (*Process, error) {
	args := []string{"funnel", fmt.Sprintf("%d", port)}
	cmd := exec.Command("tailscale", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting tailscale funnel: %w", err)
	}

	return &Process{cmd: cmd}, nil
}

func Stop(fp *Process) {
	if fp == nil || fp.cmd == nil || fp.cmd.Process == nil {
		return
	}

	log.Println("Stopping Tailscale Funnel...")
	fp.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() {
		done <- fp.cmd.Wait()
	}()

	select {
	case <-done:
		log.Println("Tailscale Funnel stopped")
	case <-time.After(5 * time.Second):
		log.Println("Tailscale Funnel did not stop, sending SIGKILL")
		fp.cmd.Process.Signal(syscall.SIGKILL)
		<-done
	}
}
