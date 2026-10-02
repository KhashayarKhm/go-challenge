package pprofserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
)

var discard = slog.New(slog.DiscardHandler)

func get(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestServesOnlyPprofEndpoints(t *testing.T) {
	s, err := Start("127.0.0.1:0", discard)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Shutdown(context.Background())
	base := "http://" + s.Addr()

	for path, want := range map[string]int{
		"/debug/pprof/":                  http.StatusOK,
		"/debug/pprof/heap?debug=1":      http.StatusOK,
		"/debug/pprof/goroutine?debug=1": http.StatusOK,
		"/debug/pprof/cmdline":           http.StatusOK,
		"/":                              http.StatusNotFound,
	} {
		if got := get(t, base+path); got != want {
			t.Errorf("GET %s = %d, want %d", path, got, want)
		}
	}
}

func TestStartFailsOnBusyPort(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	if _, err := Start(lis.Addr().String(), discard); err == nil {
		t.Fatal("expected error for a port already in use")
	}
}

func TestStartFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	// An address that cannot be bound proves nothing tried to listen.
	stop, err := StartFromEnv("invalid-address", discard)
	if err != nil {
		t.Fatalf("StartFromEnv: %v", err)
	}
	stop()
}

func TestStartFromEnvExplicitlyDisabled(t *testing.T) {
	t.Setenv(EnvEnabled, "false")
	stop, err := StartFromEnv("invalid-address", discard)
	if err != nil {
		t.Fatalf("StartFromEnv: %v", err)
	}
	stop()
}

func TestStartFromEnvEnabled(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	lis.Close()

	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvAddr, addr)
	stop, err := StartFromEnv("invalid-address", discard) // PPROF_ADDR wins over the default
	if err != nil {
		t.Fatalf("StartFromEnv: %v", err)
	}
	if got := get(t, "http://"+addr+"/debug/pprof/"); got != http.StatusOK {
		t.Fatalf("GET /debug/pprof/ = %d", got)
	}
	stop()
	if _, err := http.Get("http://" + addr + "/debug/pprof/"); err == nil {
		t.Fatal("server still reachable after stop")
	}
}

func TestStartFromEnvInvalidValue(t *testing.T) {
	t.Setenv(EnvEnabled, "yes please")
	if _, err := StartFromEnv("127.0.0.1:0", discard); err == nil {
		t.Fatal("expected error for invalid PPROF_ENABLED")
	}
}
