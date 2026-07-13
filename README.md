# bwenv

Secrets aus **Vaultwarden** / **Bitwarden** als Umgebungsvariablen in Subprozesse und Terminals
injizieren — ohne dass Secrets in Shell-History, Prozessliste (`ps`) oder Klartext-Dateien landen.

> Status: **MVP in Entwicklung.** Kernfunktionen (run/show/export, Agent, Shell-Hook) sind
> implementiert; Touch ID und Release-Automation stehen aus. Design:
> [`docs/superpowers/specs/2026-07-13-bwenv-design.md`](docs/superpowers/specs/2026-07-13-bwenv-design.md).

## Nutzung

```bash
bwenv unlock                    # einmal pro Boot: Agent starten + Session entsperren
bwenv unlock --enroll-touchid   # optional (macOS): danach entsperrt ein Touch-ID-Tap
bwenv run -- npm start          # Secrets holen und Befehl mit Env-Vars starten
eval "$(bwenv sh)"              # Variablen in die aktuelle Shell laden
bwenv show                      # geladene Var-Namen, Werte maskiert
bwenv lock                      # sofort sperren (auch automatisch nach Idle-TTL, Default 15 min)
bwenv sync                      # Vault-Änderungen vom Server holen (über den Agent)
bwenv config server <url>       # Vaultwarden-Endpunkt setzen (Passthrough an bw)
```

> **Wichtig:** Solange der Agent läuft, alle Vault-Operationen über `bwenv` machen
> (insbesondere `bwenv sync` statt `bw sync`). Parallele `bw`-Aufrufe können mit der
> Agent-Session um den rotierenden Refresh-Token konkurrieren — verliert die `bw`-CLI,
> loggt sie sich selbst aus und ein erneutes `bw login` wird nötig.

### Shell-Integration (zsh, direnv-Stil)

```bash
# in der ~/.zshrc:
eval "$(bwenv hook zsh)"

# pro Projekt einmalig freigeben:
cd ~/code/mein-projekt && bwenv allow
```

Beim Betreten eines freigegebenen Verzeichnisses mit `bwenv.yaml` werden die Secrets geladen,
beim Verlassen wieder entfernt (`unset`). **Failsafe:** offline/gesperrt/kein Agent → das
Terminal startet trotzdem sauber und lädt einfach nichts (harter Timeout, Exit 0).

### Projekt-Config (`bwenv.yaml`, gefahrlos commitbar — enthält nur Referenzen)

```yaml
version: 1

secrets:
  - env: DATABASE_URL
    item: "prod/api"          # Item-Name (oder item_id:)
    field: uri                # uri | username | password | <custom-field>
  - from:                     # bulk: jedes Custom-Field im Folder → gleichnamige Env-Var
      folder: "dev-env"
    strategy: field-name-as-env

# optional in ~/.config/bwenv/config.yaml — überall verfügbare Secrets:
global:
  - env: GITHUB_TOKEN
    item: "gh cli token"
    field: password
```

## Architektur (Kurzfassung)

- **Agent** (ssh-agent-Stil): hält die entsperrte Session **nur im RAM**, erreichbar über einen
  Unix-Socket (`0600`, Verzeichnis `0700`). Nach Reboot ist nichts entsperrt, bis `bwenv unlock`
  läuft. Auto-Lock nach Idle-TTL (`bwenv agent run --ttl`).
- **Backend:** offizielle `bw`-CLI via `bw serve` (127.0.0.1, ephemerer Port, vom Agent gekapselt).
  Self-Hosted Vaultwarden via `bwenv config server`. Ein natives Provider-Backend (eigene Krypto)
  ist hinter dem Provider-Interface nachrüstbar.
- **Trust:** Auto-Load nur in per `bwenv allow` freigegebenen Verzeichnissen — eine fremde
  `bwenv.yaml` löst nie ungefragt Vault-Zugriffe aus.
- **Hardening:** Master-Passwort nur per No-Echo-Prompt, `RLIMIT_CORE=0`, Redaction bekannter
  Secret-Werte in Fehlermeldungen.
- **Touch ID (opt-in, macOS):** `bwenv unlock --enroll-touchid` verifiziert das Master-Passwort
  gegen den Vault und legt es in der Keychain ab; jeder Unlock verlangt danach eine frische
  biometrische Prüfung (LocalAuthentication), mit Passwort-Prompt als Fallback. Default bleibt
  RAM-only. *Hinweis:* Secure-Enclave-gated Keychain-Items bräuchten ein signiertes Binary —
  unsignierte Builds nutzen das Login-Keychain-Item plus In-Prozess-Biometrie-Gate
  (siehe `internal/credstore`). Benötigt einen cgo-Build (`CGO_ENABLED=1`); ohne cgo oder auf
  Linux ist das Feature sauber deaktiviert.

**Bekannte Grenzen** (ehrlich dokumentiert): Env-Vars sind für den Owner (und root) unter
`/proc/<pid>/environ` lesbar — wie bei jeder Env-Injection (direnv, teller, bws). Gos GC
garantiert keine Speicher-Zeroization.

## Entwicklung

```bash
go build ./cmd/bwenv    # bauen
go test -race ./...     # Tests
golangci-lint run       # Lint
```

Benötigt Go ≥ 1.26 und die [`bw`-CLI](https://bitwarden.com/help/cli/) (getestet mit 2026.6.0).

## Lizenz

[MIT](LICENSE)
