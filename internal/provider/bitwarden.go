package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client spricht die REST-API eines laufenden `bw serve`-Prozesses.
type Client struct {
	baseURL string
	http    *http.Client
}

var _ Provider = (*Client)(nil)

// NewClient erzeugt einen Client für die `bw serve`-API unter baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// apiResponse ist der generische Antwort-Umschlag von `bw serve`.
type apiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type bwItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login *struct {
		Username string `json:"username"`
		Password string `json:"password"`
		URIs     []struct {
			URI string `json:"uri"`
		} `json:"uris"`
	} `json:"login"`
	Fields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"fields"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("ungültige Antwort von bw serve: %w", err)
	}
	if !envelope.Success {
		msg := envelope.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("bw serve: %s", msg)
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("ungültige Daten von bw serve: %w", err)
		}
	}
	return nil
}

// Status liefert den Sperr-Zustand des Vaults.
func (c *Client) Status(ctx context.Context) (VaultStatus, error) {
	var data struct {
		Template struct {
			Status string `json:"status"`
		} `json:"template"`
	}
	if err := c.do(ctx, http.MethodGet, "/status", nil, &data); err != nil {
		return "", err
	}
	return VaultStatus(data.Template.Status), nil
}

// Unlock entsperrt den Vault mit dem Master-Passwort.
func (c *Client) Unlock(ctx context.Context, password string) error {
	body := map[string]string{"password": password}
	return c.do(ctx, http.MethodPost, "/unlock", body, nil)
}

// Lock sperrt den Vault.
func (c *Client) Lock(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/lock", nil, nil)
}

// HealthCheck prüft, ob bw serve erreichbar ist.
func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.Status(ctx)
	return err
}

// Fetch löst alle refs auf. Items werden per Name (exakter Treffer aus der
// Suche) oder ID geholt; pro Item wird das angeforderte Feld extrahiert.
func (c *Client) Fetch(ctx context.Context, refs []SecretRef) (map[string]Secret, error) {
	result := make(map[string]Secret, len(refs))
	// Cache, damit mehrere Felder desselben Items nur einen Lookup kosten.
	byName := map[string]*bwItem{}
	byID := map[string]*bwItem{}

	for _, ref := range refs {
		item, err := c.lookupItem(ctx, ref, byName, byID)
		if err != nil {
			return nil, err
		}
		value, err := extractField(item, ref.Field)
		if err != nil {
			return nil, err
		}
		result[ref.Env] = Secret{Value: value}
	}
	return result, nil
}

func (c *Client) lookupItem(ctx context.Context, ref SecretRef, byName, byID map[string]*bwItem) (*bwItem, error) {
	if ref.ItemID != "" {
		if item, ok := byID[ref.ItemID]; ok {
			return item, nil
		}
		var item bwItem
		if err := c.do(ctx, http.MethodGet, "/object/item/"+ref.ItemID, nil, &item); err != nil {
			// Fallback für Server, die Einzel-Objekte nicht anbieten:
			// über die Liste suchen.
			found, listErr := c.findInList(ctx, "", func(it *bwItem) bool { return it.ID == ref.ItemID })
			if listErr != nil || len(found) == 0 {
				return nil, fmt.Errorf("item %q nicht gefunden: %w", ref.ItemID, err)
			}
			byID[ref.ItemID] = found[0]
			return found[0], nil
		}
		byID[ref.ItemID] = &item
		return &item, nil
	}

	if ref.Item == "" {
		return nil, fmt.Errorf("secret für %s: weder item noch item_id angegeben", ref.Env)
	}
	if item, ok := byName[ref.Item]; ok {
		return item, nil
	}
	matches, err := c.findInList(ctx, ref.Item, func(it *bwItem) bool { return it.Name == ref.Item })
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("item %q nicht gefunden", ref.Item)
	case 1:
		byName[ref.Item] = matches[0]
		return matches[0], nil
	default:
		return nil, fmt.Errorf("item %q ist mehrdeutig (%d exakte Treffer) — item_id verwenden", ref.Item, len(matches))
	}
}

func (c *Client) findInList(ctx context.Context, search string, keep func(*bwItem) bool) ([]*bwItem, error) {
	path := "/list/object/items"
	if search != "" {
		path += "?search=" + url.QueryEscape(search)
	}
	var data struct {
		Data []bwItem `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &data); err != nil {
		return nil, err
	}
	var out []*bwItem
	for i := range data.Data {
		if keep(&data.Data[i]) {
			out = append(out, &data.Data[i])
		}
	}
	return out, nil
}

func extractField(item *bwItem, field string) (string, error) {
	switch field {
	case "password":
		if item.Login != nil && item.Login.Password != "" {
			return item.Login.Password, nil
		}
	case "username":
		if item.Login != nil && item.Login.Username != "" {
			return item.Login.Username, nil
		}
	case "uri":
		if item.Login != nil && len(item.Login.URIs) > 0 {
			return item.Login.URIs[0].URI, nil
		}
	default:
		for _, f := range item.Fields {
			if f.Name == field {
				return f.Value, nil
			}
		}
	}
	return "", fmt.Errorf("item %q: feld %q nicht gefunden oder leer", item.Name, field)
}
