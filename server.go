package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type server struct {
	cfg    *Config
	httpSrv *http.Server
}

func newServer(cfg *Config) *server {
	s := &server{cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/webhook", s.handleWebhook)

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		Handler: mux,
	}

	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return s.httpSrv.Serve(ln)
}

func (s *server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.httpSrv.Shutdown(ctx)
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}
