# bwenv

Secrets aus **Vaultwarden** / **Bitwarden** als Umgebungsvariablen in Subprozesse und Terminals
injizieren — ohne dass Secrets in Shell-History, Prozessliste (`ps`) oder Klartext-Dateien landen.

> Status: **Planung / Pre-MVP.** Siehe den Design- & Implementierungsplan unter
> [`docs/superpowers/specs/2026-07-13-bwenv-design.md`](docs/superpowers/specs/2026-07-13-bwenv-design.md).

## Was es tun soll

```bash
bwenv run -- npm start          # Secrets holen und Befehl mit Env-Vars starten
eval "$(bwenv sh)"              # Variablen in die aktuelle Shell laden
bwenv show                      # geladene Var-Namen, Werte maskiert
bwenv unlock [--enroll-touchid] # Session entsperren (PW-Prompt oder Touch ID)
```

Automatisches Laden pro Projektverzeichnis (direnv-Stil) über einen Hintergrund-**Agent**, der die
entsperrte Session hält. **Failsafe:** offline/gesperrt/nicht eingeloggt → das Terminal startet
trotzdem sauber und lädt einfach nichts.

## Eckdaten

- **Sprache:** Go (Single-Binary, `goreleaser`)
- **Backend (MVP):** offizielle `bw` CLI via `bw serve`, Self-Hosted Vaultwarden via `bw config server`
- **Später:** nativer Provider (eigene Krypto) hinter demselben Provider-Interface — ohne Rewrite

## Lizenz

TBD.
