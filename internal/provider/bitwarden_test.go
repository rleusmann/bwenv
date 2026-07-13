package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// containsFold ahmt die fuzzy Suche von `bw list --search` nach.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// fakeBwServe emuliert die REST-API von `bw serve`.
func fakeBwServe(t *testing.T, status string, items []map[string]any) *httptest.Server {
	return fakeBwServeWithFolders(t, status, items, nil)
}

func fakeBwServeWithFolders(t *testing.T, status string, items []map[string]any, folders []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /list/object/folders", func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		var matched []map[string]any
		for _, f := range folders {
			name, _ := f["name"].(string)
			if search == "" || containsFold(name, search) {
				matched = append(matched, f)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"object": "list", "data": matched},
		})
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"object":   "template",
				"template": map[string]any{"status": status},
			},
		})
	})

	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"object": "message", "title": "Syncing complete."},
		})
	})

	mux.HandleFunc("POST /unlock", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Password != "correct horse" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"message": "Invalid master password.",
			})
			return
		}
		status = "unlocked"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"object": "message",
				"raw":    "session-key-123",
			},
		})
	})

	mux.HandleFunc("GET /list/object/items", func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		folderID := r.URL.Query().Get("folderid")
		var matched []map[string]any
		for _, it := range items {
			name, _ := it["name"].(string)
			if folderID != "" {
				fid, _ := it["folderId"].(string)
				if fid != folderID {
					continue
				}
			}
			if search == "" || containsFold(name, search) {
				matched = append(matched, it)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"object": "list", "data": matched},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testItems() []map[string]any {
	return []map[string]any{
		{
			"object": "item", "id": "id-1", "name": "prod/api", "type": 1,
			"login": map[string]any{
				"username": "svc-user",
				"password": "s3cret-db",
				"uris":     []map[string]any{{"uri": "postgres://db.example.com"}},
			},
			"fields": []map[string]any{
				{"name": "API_KEY", "value": "xyz-789", "type": 1},
			},
		},
		{
			"object": "item", "id": "id-2", "name": "stripe prod", "type": 1,
			"login": map[string]any{"password": "sk_live_abc"},
		},
		{
			"object": "item", "id": "id-3", "name": "stripe prod backup", "type": 1,
			"login": map[string]any{"password": "sk_live_backup"}, //nolint:gosec // G101: Test-Fixture, kein echtes Credential
		},
	}
}

func TestClientStatus(t *testing.T) {
	srv := fakeBwServe(t, "locked", nil)
	c := NewClient(srv.URL)

	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != StatusLocked {
		t.Fatalf("Status = %q, want %q", st, StatusLocked)
	}
}

func TestClientUnlock(t *testing.T) {
	srv := fakeBwServe(t, "locked", nil)
	c := NewClient(srv.URL)

	if err := c.Unlock(context.Background(), "correct horse"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != StatusUnlocked {
		t.Fatalf("Status nach Unlock = %q, want %q", st, StatusUnlocked)
	}
}

func TestClientUnlockWrongPassword(t *testing.T) {
	srv := fakeBwServe(t, "locked", nil)
	c := NewClient(srv.URL)

	err := c.Unlock(context.Background(), "wrong")
	if err == nil {
		t.Fatal("Unlock mit falschem Passwort: Fehler erwartet, bekam nil")
	}
}

func TestFetchByItemNameAndField(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", testItems())
	c := NewClient(srv.URL)

	refs := []SecretRef{
		{Env: "DATABASE_URL", Item: "prod/api", Field: "uri"},
		{Env: "DB_PASS", Item: "prod/api", Field: "password"},
		{Env: "DB_USER", Item: "prod/api", Field: "username"},
		{Env: "API_KEY", Item: "prod/api", Field: "API_KEY"},
	}
	got, err := c.Fetch(context.Background(), refs)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	want := map[string]string{
		"DATABASE_URL": "postgres://db.example.com",
		"DB_PASS":      "s3cret-db",
		"DB_USER":      "svc-user",
		"API_KEY":      "xyz-789",
	}
	for env, val := range want {
		if got[env].Value != val {
			t.Errorf("Fetch[%s] = %q, want %q", env, got[env].Value, val)
		}
	}
}

func TestFetchExactNameWinsOverPrefix(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", testItems())
	c := NewClient(srv.URL)

	got, err := c.Fetch(context.Background(), []SecretRef{
		{Env: "STRIPE_KEY", Item: "stripe prod", Field: "password"},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got["STRIPE_KEY"].Value != "sk_live_abc" {
		t.Errorf("STRIPE_KEY = %q, want sk_live_abc", got["STRIPE_KEY"].Value)
	}
}

func TestFetchByItemID(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", testItems())
	c := NewClient(srv.URL)

	got, err := c.Fetch(context.Background(), []SecretRef{
		{Env: "STRIPE_KEY", ItemID: "id-2", Field: "password"},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got["STRIPE_KEY"].Value != "sk_live_abc" {
		t.Errorf("STRIPE_KEY = %q, want sk_live_abc", got["STRIPE_KEY"].Value)
	}
}

func TestFetchItemNotFound(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", testItems())
	c := NewClient(srv.URL)

	_, err := c.Fetch(context.Background(), []SecretRef{
		{Env: "X", Item: "gibt es nicht", Field: "password"},
	})
	if err == nil {
		t.Fatal("Fetch für fehlendes Item: Fehler erwartet, bekam nil")
	}
}

func TestFetchFieldNotFound(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", testItems())
	c := NewClient(srv.URL)

	_, err := c.Fetch(context.Background(), []SecretRef{
		{Env: "X", Item: "prod/api", Field: "kein-feld"},
	})
	if err == nil {
		t.Fatal("Fetch für fehlendes Feld: Fehler erwartet, bekam nil")
	}
}

func TestFetchFolder(t *testing.T) {
	folders := []map[string]any{
		{"object": "folder", "id": "f-1", "name": "dev-env"},
		{"object": "folder", "id": "f-2", "name": "dev-env-alt"},
	}
	items := []map[string]any{
		{
			"object": "item", "id": "id-10", "name": "app secrets", "folderId": "f-1",
			"fields": []map[string]any{
				{"name": "FOO", "value": "foo-val", "type": 1},
				{"name": "BAR", "value": "bar-val", "type": 0},
			},
		},
		{
			"object": "item", "id": "id-11", "name": "more secrets", "folderId": "f-1",
			"fields": []map[string]any{
				{"name": "BAZ", "value": "baz-val", "type": 1},
			},
		},
		{
			"object": "item", "id": "id-12", "name": "anderswo", "folderId": "f-2",
			"fields": []map[string]any{
				{"name": "NOPE", "value": "nope", "type": 1},
			},
		},
	}
	srv := fakeBwServeWithFolders(t, "unlocked", items, folders)
	c := NewClient(srv.URL)

	got, err := c.FetchFolder(context.Background(), "dev-env")
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	want := map[string]string{"FOO": "foo-val", "BAR": "bar-val", "BAZ": "baz-val"}
	if len(got) != len(want) {
		t.Fatalf("FetchFolder lieferte %d Einträge, want %d: %v", len(got), len(want), got)
	}
	for env, val := range want {
		if got[env].Value != val {
			t.Errorf("FetchFolder[%s] = %q, want %q", env, got[env].Value, val)
		}
	}
}

func TestFetchFolderNotFound(t *testing.T) {
	srv := fakeBwServeWithFolders(t, "unlocked", nil, nil)
	c := NewClient(srv.URL)

	_, err := c.FetchFolder(context.Background(), "gibt es nicht")
	if err == nil {
		t.Fatal("FetchFolder für fehlenden Folder: Fehler erwartet")
	}
}

func TestClientSync(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", nil)
	c := NewClient(srv.URL)

	if err := c.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	srv.Close()
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("Sync gegen toten Server: Fehler erwartet")
	}
}

func TestHealthCheck(t *testing.T) {
	srv := fakeBwServe(t, "unlocked", nil)
	c := NewClient(srv.URL)

	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}

	srv.Close()
	if err := c.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck gegen toten Server: Fehler erwartet, bekam nil")
	}
}
