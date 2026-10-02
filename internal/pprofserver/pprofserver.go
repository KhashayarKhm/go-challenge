// Package pprofserver exposes Go runtime profiles (net/http/pprof) on a
// separate HTTP listener, for diagnosing CPU, memory and goroutine issues in a
// running ES process.
//
// It is off by default: profiles reveal internals (stack traces, command
// line) and profiling costs CPU, so it must be enabled explicitly with
// PPROF_ENABLED=true. The default address binds to localhost only; expose it
// more widely only on a trusted network.
package pprofserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"
)

// Environment variables read by StartFromEnv.
const (
	EnvEnabled = "PPROF_ENABLED"
	EnvAddr    = "PPROF_ADDR"
)

const shutdownTimeout = 5 * time.Second

// Server is a running pprof HTTP server.
type Server struct {
	srv *http.Server
	lis net.Listener
}

// Handler serves only the /debug/pprof/ endpoints. It uses its own mux rather
// than http.DefaultServeMux, so nothing else registered globally is exposed.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index) // also serves heap, goroutine, allocs, block, mutex, threadcreate
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Start listens on addr and serves profiles in the background. Listening
// happens before Start returns, so a busy port fails at startup.
func Start(addr string, log *slog.Logger) (*Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("pprof: listen %s: %w", addr, err)
	}
	// No WriteTimeout: /debug/pprof/profile and /trace stream for ?seconds=N.
	srv := &http.Server{Handler: Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("pprof server failed", "err", err)
		}
	}()
	return &Server{srv: srv, lis: lis}, nil
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string { return s.lis.Addr().String() }

// Shutdown stops the server, waiting for in-flight requests until ctx ends.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

// StartFromEnv starts the server when PPROF_ENABLED is true, on PPROF_ADDR or
// defaultAddr. The returned stop function is always safe to call; it is a
// no-op when profiling is disabled.
func StartFromEnv(defaultAddr string, log *slog.Logger) (stop func(), err error) {
	noop := func() {}

	enabled := false
	if v := os.Getenv(EnvEnabled); v != "" {
		if enabled, err = strconv.ParseBool(v); err != nil {
			return noop, fmt.Errorf("invalid %s %q: %w", EnvEnabled, v, err)
		}
	}
	if !enabled {
		return noop, nil
	}

	addr := os.Getenv(EnvAddr)
	if addr == "" {
		addr = defaultAddr
	}
	s, err := Start(addr, log)
	if err != nil {
		return noop, err
	}
	log.Info("pprof server started", "addr", s.Addr())

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			log.Error("pprof server shutdown", "err", err)
		}
	}, nil
}
