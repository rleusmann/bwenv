// Package agent implementiert den bwenv-Agent (ssh-agent-Stil): ein
// langlebiger Prozess hält die entsperrte Vault-Session im RAM und
// beantwortet Resolve-Anfragen über einen Unix-Socket (0600).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rleusmann/bwenv/internal/provider"
)

// OpenBackend öffnet das Secret-Backend mit dem Master-Passwort
// (produktiv: bw serve starten + entsperren).
type OpenBackend func(ctx context.Context, password string) (provider.Provider, func(), error)

// request ist eine Client-Anfrage an den Agent.
type request struct {
	Op       string               `json:"op"` // unlock|resolve|fetch_folder|status|lock|stop
	Password string               `json:"password,omitempty"`
	Refs     []provider.SecretRef `json:"refs,omitempty"`
	Folder   string               `json:"folder,omitempty"`
}

// response ist die Antwort des Agents.
type response struct {
	OK      bool              `json:"ok"`
	Error   string            `json:"error,omitempty"`
	Status  string            `json:"status,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

// Server hält die entsperrte Session und bedient Socket-Clients.
type Server struct {
	open OpenBackend
	ttl  time.Duration

	mu       sync.Mutex
	backend  provider.Provider
	cleanup  func()
	lastUse  time.Time
	listener net.Listener
	stopped  chan struct{}
	stopOnce sync.Once
}

// NewServer erzeugt einen Agent-Server. ttl 0 = kein Auto-Lock.
func NewServer(open OpenBackend, ttl time.Duration) *Server {
	return &Server{open: open, ttl: ttl, stopped: make(chan struct{})}
}

// Serve bedient Verbindungen bis Shutdown. Übernimmt den Listener.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.listener = l
	s.mu.Unlock()

	if s.ttl > 0 {
		go s.ttlLoop()
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-s.stopped:
				return nil
			default:
				return err
			}
		}
		go s.handleConn(conn)
	}
}

// Shutdown sperrt den Vault und beendet den Accept-Loop.
func (s *Server) Shutdown() error {
	s.stopOnce.Do(func() {
		close(s.stopped)
		s.mu.Lock()
		s.lockLocked()
		l := s.listener
		s.mu.Unlock()
		if l != nil {
			_ = l.Close()
		}
	})
	return nil
}

// Done wird geschlossen, sobald der Agent gestoppt ist.
func (s *Server) Done() <-chan struct{} { return s.stopped }

func (s *Server) ttlLoop() {
	interval := s.ttl / 4
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopped:
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.backend != nil && time.Since(s.lastUse) > s.ttl {
				s.lockLocked()
			}
			s.mu.Unlock()
		}
	}
}

// lockLocked sperrt den Vault; s.mu muss gehalten werden.
func (s *Server) lockLocked() {
	if s.cleanup != nil {
		s.cleanup()
	}
	s.backend = nil
	s.cleanup = nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	var req request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	resp := s.handle(&req)
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) handle(req *request) response {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	switch req.Op {
	case "status":
		s.mu.Lock()
		defer s.mu.Unlock()
		st := "locked"
		if s.backend != nil {
			st = "unlocked"
		}
		return response{OK: true, Status: st}

	case "unlock":
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.backend != nil {
			return response{OK: true, Status: "unlocked"}
		}
		backend, cleanup, err := s.open(ctx, req.Password)
		if err != nil {
			return response{OK: false, Error: err.Error()}
		}
		s.backend, s.cleanup, s.lastUse = backend, cleanup, time.Now()
		return response{OK: true, Status: "unlocked"}

	case "lock":
		s.mu.Lock()
		defer s.mu.Unlock()
		s.lockLocked()
		return response{OK: true, Status: "locked"}

	case "stop":
		go func() { _ = s.Shutdown() }()
		return response{OK: true}

	case "sync":
		backend, err := s.touch()
		if err != nil {
			return response{OK: false, Error: err.Error()}
		}
		syncer, ok := backend.(interface{ Sync(context.Context) error })
		if !ok {
			return response{OK: false, Error: "backend unterstützt kein sync"}
		}
		if err := syncer.Sync(ctx); err != nil {
			return response{OK: false, Error: err.Error()}
		}
		return response{OK: true}

	case "resolve", "fetch_folder":
		backend, err := s.touch()
		if err != nil {
			return response{OK: false, Error: err.Error()}
		}
		var secrets map[string]provider.Secret
		if req.Op == "resolve" {
			secrets, err = backend.Fetch(ctx, req.Refs)
		} else {
			ff, ok := backend.(interface {
				FetchFolder(context.Context, string) (map[string]provider.Secret, error)
			})
			if !ok {
				return response{OK: false, Error: "backend unterstützt keine folder-Auflösung"}
			}
			secrets, err = ff.FetchFolder(ctx, req.Folder)
		}
		if err != nil {
			return response{OK: false, Error: err.Error()}
		}
		out := make(map[string]string, len(secrets))
		for k, v := range secrets {
			out[k] = v.Value
		}
		return response{OK: true, Secrets: out}

	default:
		return response{OK: false, Error: fmt.Sprintf("unbekannte op %q", req.Op)}
	}
}

// touch liefert das Backend und aktualisiert den Idle-Zeitstempel.
func (s *Server) touch() (provider.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend == nil {
		return nil, errors.New("vault ist gesperrt — `bwenv unlock` ausführen")
	}
	s.lastUse = time.Now()
	return s.backend, nil
}

// Listen erzeugt den Unix-Socket mit 0700-Verzeichnis und 0600-Socket
// und ersetzt dabei stale Socket-Dateien.
func Listen(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Falls ein alter Agent den Socket hinterlassen hat: prüfen, ob er lebt.
	if _, err := os.Stat(path); err == nil {
		conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("agent läuft bereits (%s)", path)
		}
		_ = os.Remove(path)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// SocketPath liefert den Agent-Socket-Pfad: BWENV_AGENT_SOCKET-Override,
// sonst $XDG_RUNTIME_DIR/bwenv/agent.sock, sonst ~/.bwenv/agent.sock.
func SocketPath() (string, error) {
	if p := os.Getenv("BWENV_AGENT_SOCKET"); p != "" {
		return p, nil
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "bwenv", "agent.sock"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bwenv", "agent.sock"), nil
}
