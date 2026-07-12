package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/rleusmann/bwenv/internal/provider"
)

// Client spricht das Agent-Protokoll über den Unix-Socket.
// Er implementiert provider.Provider und die Folder-Auflösung, sodass er
// im Resolver an Stelle des direkten Backends verwendet werden kann.
type Client struct {
	path string
}

var _ provider.Provider = (*Client)(nil)

// NewAgentClient erzeugt einen Client für den Socket unter path.
func NewAgentClient(path string) *Client {
	return &Client{path: path}
}

func (c *Client) roundTrip(ctx context.Context, req *request) (*response, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	}

	// Das Master-Passwort geht bewusst über den 0600-Unix-Socket zum Agent
	// (Plan §3.3) — nie über CLI-Args, Env oder Netzwerk.
	if err := json.NewEncoder(conn).Encode(req); err != nil { //nolint:gosec // G117: siehe Kommentar
		return nil, err
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return &resp, nil
}

// Available meldet, ob unter path ein Agent antwortet.
func (c *Client) Available(ctx context.Context) bool {
	_, err := c.roundTrip(ctx, &request{Op: "status"})
	return err == nil
}

// Status liefert locked|unlocked.
func (c *Client) Status(ctx context.Context) (string, error) {
	resp, err := c.roundTrip(ctx, &request{Op: "status"})
	if err != nil {
		return "", err
	}
	return resp.Status, nil
}

// Unlock entsperrt den Agent mit dem Master-Passwort.
func (c *Client) Unlock(ctx context.Context, password string) error {
	_, err := c.roundTrip(ctx, &request{Op: "unlock", Password: password})
	return err
}

// Lock sperrt den Agent sofort.
func (c *Client) Lock(ctx context.Context) error {
	_, err := c.roundTrip(ctx, &request{Op: "lock"})
	return err
}

// StopAgent beendet den Agent-Prozess.
func (c *Client) StopAgent(ctx context.Context) error {
	_, err := c.roundTrip(ctx, &request{Op: "stop"})
	return err
}

// Fetch implementiert provider.Provider über den Agent.
func (c *Client) Fetch(ctx context.Context, refs []provider.SecretRef) (map[string]provider.Secret, error) {
	resp, err := c.roundTrip(ctx, &request{Op: "resolve", Refs: refs})
	if err != nil {
		return nil, err
	}
	return toSecrets(resp.Secrets), nil
}

// FetchFolder implementiert die Bulk-Auflösung über den Agent.
func (c *Client) FetchFolder(ctx context.Context, folder string) (map[string]provider.Secret, error) {
	resp, err := c.roundTrip(ctx, &request{Op: "fetch_folder", Folder: folder})
	if err != nil {
		return nil, err
	}
	return toSecrets(resp.Secrets), nil
}

// HealthCheck implementiert provider.Provider.
func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.roundTrip(ctx, &request{Op: "status"})
	return err
}

func toSecrets(m map[string]string) map[string]provider.Secret {
	out := make(map[string]provider.Secret, len(m))
	for k, v := range m {
		out[k] = provider.Secret{Value: v}
	}
	return out
}
