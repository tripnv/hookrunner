package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type FunnelProcess struct {
	cmd *exec.Cmd
}

func startFunnel(port int, urlOverride string) (*FunnelProcess, error) {
	args := []string{"funnel", fmt.Sprintf("%d", port)}
	cmd := exec.Command("tailscale", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting tailscale funnel: %w", err)
	}

	return &FunnelProcess{cmd: cmd}, nil
}

func stopFunnel(fp *FunnelProcess) {
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
