package resolver

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/provider"
)

// fakeProvider löst Refs aus einer statischen Tabelle auf.
type fakeProvider struct {
	// key: "<item-oder-id>/<field>" → Wert
	values map[string]string
	// key: Folder-Name → EnvName → Wert
	folders map[string]map[string]string
}

func (f *fakeProvider) Fetch(_ context.Context, refs []provider.SecretRef) (map[string]provider.Secret, error) {
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

func (f *fakeProvider) HealthCheck(context.Context) error { return nil }

func (f *fakeProvider) FetchFolder(_ context.Context, folder string) (map[string]provider.Secret, error) {
	envs, ok := f.folders[folder]
	if !ok {
		return nil, errors.New("folder nicht gefunden: " + folder)
	}
	out := map[string]provider.Secret{}
	for env, val := range envs {
		out[env] = provider.Secret{Value: val}
	}
	return out, nil
}

func newFake() *fakeProvider {
	return &fakeProvider{
		values: map[string]string{
			"prod/api/uri":         "postgres://db",
			"prod/api/password":    "s3cret",
			"id-2/password":        "sk_live_abc",
			"gh cli token/password": "ghp_xyz",
		},
		folders: map[string]map[string]string{
			"dev-env": {"FOO": "foo-val", "BAR": "bar-val"},
		},
	}
}

func TestResolveSingleEntries(t *testing.T) {
	entries := []config.SecretEntry{
		{Env: "DATABASE_URL", Item: "prod/api", Field: "uri"},
		{Env: "STRIPE_KEY", ItemID: "id-2", Field: "password"},
	}
	env, err := Resolve(context.Background(), newFake(), entries)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if env["DATABASE_URL"] != "postgres://db" || env["STRIPE_KEY"] != "sk_live_abc" {
		t.Errorf("EnvMap = %v", env)
	}
}

func TestResolveBulkFolder(t *testing.T) {
	entries := []config.SecretEntry{
		{From: &config.From{Folder: "dev-env"}, Strategy: "field-name-as-env"},
	}
	env, err := Resolve(context.Background(), newFake(), entries)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if env["FOO"] != "foo-val" || env["BAR"] != "bar-val" {
		t.Errorf("EnvMap = %v", env)
	}
}

func TestResolveLaterEntriesWin(t *testing.T) {
	entries := []config.SecretEntry{
		{From: &config.From{Folder: "dev-env"}, Strategy: "field-name-as-env"},
		{Env: "FOO", Item: "prod/api", Field: "password"}, // überschreibt bulk-FOO
	}
	env, err := Resolve(context.Background(), newFake(), entries)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if env["FOO"] != "s3cret" {
		t.Errorf("FOO = %q, want s3cret (expliziter Eintrag muss bulk überschreiben)", env["FOO"])
	}
}

func TestResolveErrorPropagates(t *testing.T) {
	entries := []config.SecretEntry{
		{Env: "X", Item: "fehlt", Field: "password"},
	}
	_, err := Resolve(context.Background(), newFake(), entries)
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
}

func TestResolveBulkWithoutFolderSupport(t *testing.T) {
	// Provider, der das FolderFetcher-Interface nicht implementiert.
	type plainProvider struct{ provider.Provider }
	p := plainProvider{Provider: newFake()}

	entries := []config.SecretEntry{
		{From: &config.From{Folder: "dev-env"}, Strategy: "field-name-as-env"},
	}
	_, err := Resolve(context.Background(), p, entries)
	if err == nil {
		t.Fatal("Fehler erwartet, wenn Provider kein Bulk unterstützt")
	}
}
