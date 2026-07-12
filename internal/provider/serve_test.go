package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Als Fake-`bw serve` laufen, wenn wir als Subprozess gestartet wurden.
	if os.Getenv("BWENV_FAKE_BW") == "1" {
		runFakeBwServeProcess()
		return
	}
	os.Exit(m.Run())
}

// runFakeBwServeProcess emuliert `bw serve --hostname H --port P` im Testbinary.
func runFakeBwServeProcess() {
	var host, port string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--hostname":
			i++
			host = args[i]
		case "--port":
			i++
			port = args[i]
		}
	}
	if host == "" || port == "" {
		fmt.Fprintln(os.Stderr, "fake bw: --hostname/--port fehlen")
		os.Exit(2)
	}
	// Readiness-Verzögerung simulieren.
	time.Sleep(150 * time.Millisecond)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"template": map[string]any{"status": "locked"},
			},
		})
	})
	srv := &http.Server{Addr: host + ":" + port, Handler: mux, ReadHeaderTimeout: time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fake bw:", err)
		os.Exit(1)
	}
}

func TestStartServeLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := StartServe(ctx, ServeOptions{
		BwPath: os.Args[0],
		Env:    append(os.Environ(), "BWENV_FAKE_BW=1"),
	})
	if err != nil {
		t.Fatalf("StartServe: %v", err)
	}
	defer func() { _ = s.Close() }()

	st, err := s.Client().Status(ctx)
	if err != nil {
		t.Fatalf("Status gegen gestarteten Serve: %v", err)
	}
	if st != StatusLocked {
		t.Fatalf("Status = %q, want %q", st, StatusLocked)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Nach Close darf der Server nicht mehr erreichbar sein.
	if err := s.Client().HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck nach Close: Fehler erwartet, Server läuft noch")
	}
}

func TestStartServeFailsWhenProcessDies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := StartServe(ctx, ServeOptions{
		BwPath: "/usr/bin/false",
		Env:    os.Environ(),
	})
	if err == nil {
		t.Fatal("StartServe mit sofort sterbendem Prozess: Fehler erwartet, bekam nil")
	}
}
