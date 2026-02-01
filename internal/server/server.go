package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Server struct {
	httpSrv *http.Server
}

func New(port int, webhookHandler http.Handler) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/webhook", webhookHandler)

	return &Server{
		httpSrv: &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", port),
			Handler: mux,
		},
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return s.httpSrv.Serve(ln)
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.httpSrv.Shutdown(ctx)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}
