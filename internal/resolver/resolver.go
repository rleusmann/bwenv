// Package resolver mappt Config-Einträge auf Provider-Refs und baut die EnvMap.
package resolver

import (
	"context"
	"fmt"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/provider"
)

// EnvMap ist das Ergebnis der Auflösung: Env-Name → Klartext-Wert.
type EnvMap map[string]string

// FolderFetcher können Provider implementieren, die Bulk-Auflösung
// per Folder unterstützen.
type FolderFetcher interface {
	FetchFolder(ctx context.Context, folder string) (map[string]provider.Secret, error)
}

// Resolve löst alle entries gegen p auf. Spätere Einträge überschreiben
// frühere (explizite Einträge können so Bulk-Ergebnisse übersteuern).
func Resolve(ctx context.Context, p provider.Provider, entries []config.SecretEntry) (EnvMap, error) {
	env := EnvMap{}
	for i, e := range entries {
		if e.From != nil {
			ff, ok := p.(FolderFetcher)
			if !ok {
				return nil, fmt.Errorf("eintrag %d: provider unterstützt keine from/folder-Auflösung", i)
			}
			secrets, err := ff.FetchFolder(ctx, e.From.Folder)
			if err != nil {
				return nil, fmt.Errorf("folder %q: %w", e.From.Folder, err)
			}
			for name, s := range secrets {
				env[name] = s.Value
			}
			continue
		}

		secrets, err := p.Fetch(ctx, []provider.SecretRef{{
			Env:    e.Env,
			Item:   e.Item,
			ItemID: e.ItemID,
			Field:  e.Field,
		}})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Env, err)
		}
		env[e.Env] = secrets[e.Env].Value
	}
	return env, nil
}
