package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rleusmann/bwenv/internal/provider"
)

type fakeBackend struct {
	values    map[string]string // "<item>/<field>" → Wert
	syncCalls atomic.Int32
}

func (f *fakeBackend) Sync(context.Context) error {
	f.syncCalls.Add(1)
	return nil
}

func (f *fakeBackend) Fetch(_ context.Context, refs []provider.SecretRef) (map[string]provider.Secret, error) {
	out := map[string]provider.Secret{}
	for _, ref := range refs {
		item := ref.Item
		if ref.ItemID != "" {
			item = ref.ItemID
		}
		val, ok := f.values[item+"/"+ref.Field]
		if !ok {
			return nil, fmt.Errorf("nicht gefunden: %s/%s", item, ref.Field)
		}
		out[ref.Env] = provider.Secret{Value: val}
	}
	return out, nil
}

func (f *fakeBackend) HealthCheck(context.Context) error { return nil }

func (f *fakeBackend) FetchFolder(_ context.Context, folder string) (map[string]provider.Secret, error) {
	if folder != "dev-env" {
		return nil, errors.New("folder nicht gefunden")
	}
	return map[string]provider.Secret{"FOO": {Value: "foo-val"}}, nil
}

// shortSocketDir liefert ein kurzes Temp-Verzeichnis: macOS begrenzt
// Unix-Socket-Pfade auf 104 Bytes, t.TempDir() ist dafür oft zu lang.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bwt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startTestAgent startet einen Agent-Server auf einem Temp-Socket.
// cleanupCount zählt, wie oft das Backend geschlossen wurde.
func startTestAgent(t *testing.T, ttl time.Duration, cleanupCount *atomic.Int32) *Client {
	t.Helper()
	open := func(_ context.Context, password string) (provider.Provider, func(), error) {
		if password != "correct horse" {
			return nil, nil, errors.New("falsches Master-Passwort")
		}
		return &fakeBackend{values: map[string]string{
			"app/password": "geheim",
		}}, func() { cleanupCount.Add(1) }, nil
	}

	sock := filepath.Join(shortSocketDir(t), "agent.sock")
	srv := NewServer(open, ttl)
	l, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return NewAgentClient(sock)
}

func TestAgentStartsLocked(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)

	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != "locked" {
		t.Errorf("Status = %q, want locked", st)
	}
}

func TestAgentUnlockAndResolve(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.Unlock(ctx, "correct horse"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	st, _ := c.Status(ctx)
	if st != "unlocked" {
		t.Fatalf("Status nach Unlock = %q", st)
	}

	got, err := c.Fetch(ctx, []provider.SecretRef{{Env: "FOO", Item: "app", Field: "password"}})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got["FOO"].Value != "geheim" {
		t.Errorf("FOO = %q, want geheim", got["FOO"].Value)
	}
}

func TestAgentWrongPasswordStaysLocked(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.Unlock(ctx, "falsch"); err == nil {
		t.Fatal("Unlock mit falschem Passwort: Fehler erwartet")
	}
	st, _ := c.Status(ctx)
	if st != "locked" {
		t.Errorf("Status = %q, want locked", st)
	}
}

func TestAgentResolveWhileLockedFails(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)

	_, err := c.Fetch(context.Background(), []provider.SecretRef{{Env: "X", Item: "app", Field: "password"}})
	if err == nil {
		t.Fatal("Fetch bei gesperrtem Agent: Fehler erwartet")
	}
}

func TestAgentFetchFolder(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.Unlock(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	got, err := c.FetchFolder(ctx, "dev-env")
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	if got["FOO"].Value != "foo-val" {
		t.Errorf("FOO = %q", got["FOO"].Value)
	}
}

func TestAgentLockClosesBackend(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.Unlock(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := c.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if n.Load() != 1 {
		t.Errorf("Backend-Cleanup %d-mal aufgerufen, want 1", n.Load())
	}
	st, _ := c.Status(ctx)
	if st != "locked" {
		t.Errorf("Status = %q, want locked", st)
	}
}

func TestAgentIdleTTLAutoLocks(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 80*time.Millisecond, &n)
	ctx := context.Background()

	if err := c.Unlock(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := c.Status(ctx)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st == "locked" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Idle-TTL hat nicht gelockt")
		}
		time.Sleep(30 * time.Millisecond)
	}
	if n.Load() != 1 {
		t.Errorf("Backend-Cleanup %d-mal aufgerufen, want 1", n.Load())
	}
}

func TestAgentSyncDelegatesToBackend(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.Sync(ctx); err == nil {
		t.Fatal("Sync bei gesperrtem Agent: Fehler erwartet")
	}
	if err := c.Unlock(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestAgentStopShutsDown(t *testing.T) {
	var n atomic.Int32
	c := startTestAgent(t, 0, &n)
	ctx := context.Background()

	if err := c.StopAgent(ctx); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	// Danach darf der Socket nicht mehr antworten.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := c.Status(ctx); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent antwortet nach Stop weiterhin")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestListenSetsRestrictivePermissions(t *testing.T) {
	dir := filepath.Join(shortSocketDir(t), "sub")
	sock := filepath.Join(dir, "agent.sock")

	l, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("Socket-Dir-Rechte = %o, want 700", perm)
	}
	sockInfo, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := sockInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("Socket-Rechte = %o, want 600", perm)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	sock := filepath.Join(shortSocketDir(t), "agent.sock")
	l1, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	_ = l1.Close() // hinterlässt ggf. stale File

	l2, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen über stale Socket: %v", err)
	}
	_ = l2.Close()
}

func TestSocketPathPrefersEnvOverride(t *testing.T) {
	t.Setenv("BWENV_AGENT_SOCKET", "/tmp/custom.sock")
	got, err := SocketPath()
	if err != nil || got != "/tmp/custom.sock" {
		t.Errorf("SocketPath = %q, %v", got, err)
	}
}

func TestSocketPathUsesXDGRuntimeDir(t *testing.T) {
	t.Setenv("BWENV_AGENT_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/501")
	got, err := SocketPath()
	if err != nil || got != "/run/user/501/bwenv/agent.sock" {
		t.Errorf("SocketPath = %q, %v", got, err)
	}
}
