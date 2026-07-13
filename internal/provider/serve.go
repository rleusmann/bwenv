package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// ServeOptions konfiguriert den zu startenden `bw serve`-Prozess.
type ServeOptions struct {
	BwPath string        // Pfad zum bw-Binary; default "bw"
	Env    []string      // Prozess-Env (z. B. mit BW_SESSION); nil = geerbt
	Ready  time.Duration // max. Wartezeit auf Readiness; default 15s
}

// Serve ist ein laufender, von bwenv verwalteter `bw serve`-Prozess.
type Serve struct {
	cmd    *exec.Cmd
	client *Client
	done   chan error
}

// StartServe startet `bw serve` auf 127.0.0.1 mit ephemerem Port und wartet,
// bis die API antwortet. Der Prozess wird mit Close beendet.
func StartServe(ctx context.Context, opts ServeOptions) (*Serve, error) {
	if opts.BwPath == "" {
		opts.BwPath = "bw"
	}
	if opts.Ready == 0 {
		opts.Ready = 15 * time.Second
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("kein freier port: %w", err)
	}

	// Kein CommandContext: Teardown übernimmt Close (SIGTERM statt SIGKILL).
	// BwPath ist bewusst konfigurierbar (User-eigene bw-Installation, Tests).
	cmd := exec.Command(opts.BwPath, "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)) //nolint:gosec // G204: Pfad kommt aus lokaler Config, nicht aus Fremdeingabe

	cmd.Env = opts.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("bw serve starten: %w", err)
	}

	s := &Serve{
		cmd:    cmd,
		client: NewClient("http://127.0.0.1:" + strconv.Itoa(port)),
		done:   make(chan error, 1),
	}
	go func() { s.done <- cmd.Wait() }()

	if err := s.waitReady(ctx, opts.Ready); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Client liefert den HTTP-Client für diesen Serve-Prozess.
func (s *Serve) Client() *Client { return s.client }

// Close beendet den Prozess (SIGTERM, nach 3s SIGKILL) und wartet auf Exit.
func (s *Serve) Close() error {
	if s.cmd.Process == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil // bereits beendet
	default:
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return nil // Prozess schon weg
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
		return nil
	}
}

func (s *Serve) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		select {
		case err := <-s.done:
			s.done <- err // für Close wieder bereitstellen
			return fmt.Errorf("bw serve unerwartet beendet: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := s.client.HealthCheck(probeCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("bw serve wurde nicht rechtzeitig bereit")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// freePort reserviert kurz einen ephemeren TCP-Port und gibt ihn frei.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
