# bwenv — Design & Implementierungsplan

**Datum:** 2026-07-13
**Status:** Entwurf, zur Review
**Arbeitsname:** `bwenv` (Bitwarden → Environment)

---

## 1. Problem & Ziel

Secrets aus einem selbstgehosteten **Vaultwarden** (bzw. Bitwarden) als Umgebungsvariablen
in Subprozesse und Terminals injizieren — ohne dass Secrets in Shell-History,
Prozessliste (`ps`) oder Klartext-Dateien landen.

Es existiert kein Open-Source-CLI, das das für Bitwarden/Vaultwarden tut:

- `bws run` — proprietär, kein Vaultwarden.
- `teller run` — Open Source, aber Bitwarden-Feature-Request seit 2023 offen.
- `sops exec-env` — Git-basiert, kein zentraler Vault.
- `infisical run` — eigener Server statt Vaultwarden.

**Ziel:** persönliches Tool, das aber von Anfang an so strukturiert ist, dass ein Ausbau
zum vollwertigen OSS-Projekt (Multi-Provider, Tests, CI) **ohne Rewrite** möglich ist.

---

## 2. Kernentscheidungen

| Entscheidung | Wahl | Begründung |
|---|---|---|
| **Sprache** | **Go** | Bestes Single-Binary + Cross-Compiling (`goreleaser`), reifes CLI-Ökosystem (cobra/viper), schnelle Solo-Iteration. Variante A braucht null Krypto. |
| **Backend-Zugriff (MVP)** | **Variante A via `bw serve`** | Kein eigener sicherheitskritischer Krypto-Code; `bw` übernimmt Login/KDF/Entschlüsselung. `bw serve` = lokaler REST-Server → kein Prozess-Spawn pro Secret. |
| **Backend-Zugriff (später)** | **Variante B als neuer Provider** | Native Krypto gegen die Bitwarden-API. Hinter dem Provider-Interface → kein Rewrite. |
| **Session-Sharing** | **bwenv-Agent** (ssh-agent-Stil) | Langlebiger Hintergrundprozess hält die entsperrte Session; Terminals sprechen über Unix-Socket. |
| **Auto-Load-Scope** | **Pro-Verzeichnis (direnv-Stil) + optionale globale Sektion** | Minimale Exposition: Secrets nur da, wo eine `bwenv.yaml` liegt, plus optionale „überall"-Secrets. |
| **Unlock** | **Manuell pro Boot (RAM-only Default) + optionaler Touch-ID-Opt-in** | Nichts Sensibles überlebt den Reboot; Touch ID als bewusst freigeschalteter Komfort. |
| **Config-Format** | **YAML** | Ökosystem-Standard (Teller/Bitwarden), gut lesbare Listen, viper-nativ. |

### 2.1 Warum kein Rust (Trade-off)

Rust wäre nur überlegen, wenn **Variante B ein festes Ziel** wäre (offizielles Bitwarden-SDK
in Rust + Vaultwarden-Quellcode als Referenz). Da Variante A den MVP trägt und Variante B
hinter dem Provider-Interface nachrüstbar ist, gewinnt Go bei Iterationsgeschwindigkeit und
Distribution. In Go ist Variante B mit `golang.org/x/crypto` (PBKDF2, Argon2id, AES-CBC, HMAC)
machbar — nicht geschenkt, aber Standard-Primitiven.

### 2.2 Zur Frage „braucht man eine Bitwarden-Bibliothek?"

- **MVP (Variante A):** Nein. `bw serve` *ist* die Bibliothek. Jede Sprache mit HTTP-Client + `exec` reicht.
- **Variante B (später):** Ja. Einzige ernsthaft gepflegte Krypto-Referenz ist das offizielle
  Bitwarden-SDK (Rust) + Vaultwarden. In Go/Python nur Community-Reimplementierungen.

---

## 3. Architektur

### 3.1 Komponenten

Jede Komponente hat genau einen Zweck und ist isoliert testbar.

1. **Config-Parser** — liest `bwenv.yaml`, validiert Schema, löst Env-Referenzen (`${VAR}`)
   für Server/Email auf. Passwörter niemals aus Datei.
2. **Provider-Interface** — der „kein Rewrite"-Garant:
   ```
   type Provider interface {
       Fetch(ctx context.Context, refs []SecretRef) (map[string]Secret, error)
       HealthCheck(ctx context.Context) error
   }
   ```
   Impl. 1: `BitwardenProvider` (heute). Impl. 2: `BitwardenNativeProvider` (Variante B, später).
3. **BitwardenProvider** — kapselt `bw serve`: startet Prozess auf `127.0.0.1` mit ephemerem
   Port, wartet auf Readiness, entsperrt Session, spricht HTTP, räumt per `defer` auf.
   Self-Hosted via `bw config server <url>`.
4. **Resolver** — mappt Config-Einträge (Item-Name/-ID + Feld, oder bulk per Folder/Tag) auf
   `SecretRef`s und ruft den Provider. Erzeugt die `EnvMap`.
5. **Agent** — langlebiger Hintergrundprozess (Details §3.3).
6. **Runner** — baut Env, `exec.Command` mit vererbtem stdio, Signal-Forwarding
   (SIGINT/SIGTERM), Exit-Code-Passthrough. Optional `syscall.Exec` (kein bwenv-Parent in `ps`).
7. **Formatter** — `show` (maskiert), `sh`/`zsh` (Shell-korrektes `export` mit Escaping).
8. **Shell-Integration** — emittiert Hook-Snippet für `.zshrc` (Details §3.4).
9. **Session-/Trust-Store** — Agent-Socket-Pfad, Allowlist erlaubter Auto-Load-Verzeichnisse,
   optionales Touch-ID-Keychain-Credential.

### 3.2 Datenfluss

```
bwenv run -- npm start
      │
      ▼
Config-Parser ──▶ Resolver ──▶ Agent (Unix-Socket) ──▶ BitwardenProvider ──▶ bw serve ──▶ Vaultwarden
                                     │
                                     ▼
                                  EnvMap ──▶ Runner (exec + env)  /  Formatter (sh|show)
```

Beim Auto-Load im Terminal geht der Weg über den Agent, der die entsperrte Session bereits hält —
kein erneutes Unlock, kein `bw serve`-Neustart pro Terminal.

### 3.3 Agent (ssh-agent-Stil)

- **Zweck:** hält die entsperrte Session zentral, damit alle Terminals ohne erneutes Master-Passwort
  laden können.
- **Kommunikation:** Unix Domain Socket, `$XDG_RUNTIME_DIR/bwenv/agent.sock`
  (macOS-Fallback `~/.bwenv/agent.sock`), Ordner `0700`, Socket **`0600`** — nur der eigene User.
  Ersetzt den auth-losen `bw serve`-TCP-Port und schließt damit das Shared-Host-Risiko.
- **Verantwortung:** verwaltet intern die `bw serve`-Session (bzw. `BW_SESSION`), cached
  entschlüsselte Items **nur im Speicher**, beantwortet Client-Requests „resolve diese Refs".
- **Auto-Lock:** relockt nach Idle-TTL → danach degradieren Hooks sauber (laden nichts).
- **Lifecycle:** siehe §5 (Unlock-/Lock-Lifecycle).
- **Upgrade-Path:** Session-Store ist abstrahiert; eine spätere Keychain-only-Variante ohne Daemon
  bliebe möglich.

### 3.4 Shell-Integration & Failsafe

- `bwenv hook zsh` emittiert das Snippet für `.zshrc` (analog `direnv hook zsh`, `zoxide init`).
- **Pro-Verzeichnis:** `chpwd`/`precmd`-Hook sucht aufwärts nach `bwenv.yaml`. Gefunden **und**
  Verzeichnis in Allowlist → `eval "$(bwenv export --format=zsh --silent --timeout=300ms)"`.
  Beim Verlassen des Verzeichnisses werden die geladenen Var-Namen wieder `unset` (direnv-Stil).
- **Global:** optionale `global:`-Sektion wird einmal beim Shell-Init geladen.
- **Trust:** `bwenv allow` / `bwenv deny` pflegt die Allowlist — eine fremde `bwenv.yaml` löst
  nie ungefragt Vault-Zugriffe aus.
- **Failsafe (kritisch):** `bwenv export` bei *jedem* Problem — Socket fehlt, Agent aus, Vault locked,
  Server offline — gibt **nichts** aus und exitet mit **0**. Harter Timeout (300 ms default), damit
  ein langsamer/offline Vaultwarden das Terminal nie blockiert. Optional dezenter Ein-Zeilen-Hinweis
  auf stderr (abschaltbar via `BWENV_QUIET`).

### 3.5 CLI-Oberfläche

```
bwenv run -- <cmd> [args...]       # Secrets injizieren und Befehl starten
bwenv sh | export                  # Ausgabe für eval "$(bwenv sh)"
bwenv show                         # geladene Var-Namen, Werte maskiert
bwenv unlock [--enroll-touchid]    # Session entsperren (PW-Prompt oder Touch ID)
bwenv lock                         # sofort relocken
bwenv agent [stop|status]          # Agent-Lifecycle
bwenv hook zsh                     # Shell-Integrations-Snippet ausgeben
bwenv allow | deny                 # Verzeichnis für Auto-Load erlauben/sperren
bwenv config server <url>          # Vaultwarden-Endpunkt setzen
```

---

## 4. Config-Format

```yaml
# bwenv.yaml
version: 1

provider:
  type: bitwarden
  server: https://vault.leusmann.com   # Vaultwarden; leer = bitwarden.com
  email: ${BWENV_EMAIL}                # aus Env; niemals Passwort in Datei

secrets:
  - env: DATABASE_URL
    item: "prod/api"          # Item-Name (oder item_id:)
    field: uri                # login.uri | login.username | password | <custom-field>
  - env: STRIPE_KEY
    item: "stripe prod"
    field: password
  - from:                     # bulk
      folder: "dev-env"
    strategy: field-name-as-env   # jedes Custom-Field → gleichnamige Env-Var

# optional: überall-Secrets (Shell-Init statt chpwd)
global:
  - env: GITHUB_TOKEN
    item: "gh cli token"
    field: password
```

---

## 5. Unlock-/Lock-Lifecycle

Kernprinzip: **Session-Key und entschlüsselte Secrets leben nur im Agent-RAM.**

- **Kein Auto-Start beim Login.** Nach einem Reboot ist der Agent weg, RAM gelöscht → es existiert
  **kein** entschlüsseltes Secret mehr.
- **Bis zum ersten `bwenv unlock` laden alle Terminals nichts** (Failsafe greift: Socket fehlt →
  exit 0, still). Terminal funktioniert, nur ohne Secrets.
- **Einmal `bwenv unlock` pro Boot:** No-Echo-Master-Passwort-Prompt → Agent spawnt, Session im RAM.
  Ab dann laden alle Terminals in erlaubten Verzeichnissen automatisch.

### 5.1 Lock-Trigger

| Trigger | Verhalten | Default |
|---|---|---|
| **Reboot** | RAM weg → Auto-Lock, erzwingt manuelles Unlock | immer an (systembedingt) |
| **Idle-TTL** | Nach X Minuten ohne Zugriff relockt der Agent | 15 min, konfigurierbar / abschaltbar |
| **Manuell** | `bwenv lock` sofort | jederzeit |
| **Sleep/Suspend** (optional, später) | Lock beim Ruhezustand | aus, opt-in |

### 5.2 Touch-ID-Unlock (macOS, opt-in)

Touch ID liefert selbst keinen Session-Key. bwenv legt beim Enrollment ein **Unlock-Credential
in die macOS-Keychain**, geschützt durch biometrische Access-Control (Secure Enclave):

```
bwenv unlock --enroll-touchid   # einmalig: Master-PW eingeben → Credential in Keychain
bwenv unlock                    # danach: Touch-ID-Tap → Agent bekommt Credential → entsperrt
```

- **Gespeichertes Credential:** das **Master-Passwort** (robust auch nach server-seitigem
  Session-Invalidieren; so löst es auch Bitwardens Desktop-App).
- **Access-Control-Flags:** `biometryCurrentSet` + `ThisDeviceOnly` (nicht in iCloud gesynct) +
  Secure Enclave. Ein Reboot legt das Credential **nicht** offen — jeder Unlock braucht einen
  frischen Fingerabdruck.
- **Modus:** **Opt-in**. Default bleibt RAM-only (Master-Passwort tippen). Touch ID wird bewusst
  freigeschaltet.
- **Fallback:** schlägt Touch ID fehl oder ist nicht verfügbar (z. B. Linux) → Master-Passwort-Prompt.
  Funktion ist per Build-Tag macOS-only (cgo gegen Security.framework / LocalAuthentication).

**Trade-off (bewusst):** Im Touch-ID-Modus existiert ein persistentes, verschlüsseltes +
biometrie-gated Credential, das den Reboot überlebt — im Gegensatz zum RAM-only-Default, bei dem
buchstäblich nichts überlebt. Beide Modi behalten Idle-TTL-Auto-Lock und `bwenv lock`.

---

## 6. Datenablage

**bwenv legt global nur Nicht-Geheimes ab. Secrets und Session-Key berühren nie die Platte**
(einzige Ausnahme: das opt-in Touch-ID-Credential in der Secure-Enclave-gated Keychain).

| Was | Ort | Sensibel? | Zweck |
|---|---|---|---|
| Globale Config | `~/.config/bwenv/config.yaml` (respektiert `$XDG_CONFIG_HOME`) | nein | Provider-Defaults, `global:`-Referenzen (keine Werte) |
| Trust-Allowlist | `~/.config/bwenv/allow/` (Hash je Pfad, direnv-Stil) | nein | Welche Verzeichnisse dürfen auto-laden |
| Agent-Socket | `$XDG_RUNTIME_DIR/bwenv/agent.sock`, macOS-Fallback `~/.bwenv/agent.sock` (`0700`/`0600`) | Zugang! | IPC Terminal ↔ Agent |
| **Session-Key** | **nur Agent-RAM** | **ja** | Wird nie persistiert |
| **Entschlüsselte Secrets** | **nur Agent-RAM** | **ja** | Cache mit Idle-TTL |
| Touch-ID-Credential (opt-in) | macOS-Keychain, biometrie-gated, ThisDeviceOnly | ja, geschützt | Master-Passwort hinter Secure Enclave |
| Verschlüsselter Vault | `~/.config/Bitwarden CLI/data.json` | verschlüsselt | Verwaltet **`bw`**, nicht bwenv |

- **Pro-Projekt-`bwenv.yaml`** ist *nicht* global — sie liegt im Projektordner und enthält nur
  Item-*Referenzen* (keine Werte), ist also gefahrlos commit-bar.
- **macOS-Detail:** `$XDG_RUNTIME_DIR` fehlt standardmäßig → Fallback `~/.bwenv/` mit `0700`.
  Config folgt `~/.config/bwenv` (CLI-Konvention), respektiert aber `$XDG_CONFIG_HOME`.

---

## 7. Sicherheitsanforderungen

- **Master-Passwort:** nur No-Echo-Prompt (`golang.org/x/term`) beim `bwenv unlock`, oder
  `bw unlock --passwordenv` mit einer von bwenv gesetzten, sofort gelöschten Env-Var. Nie als
  CLI-Arg (→ `ps`), nie geloggt.
- **Session-Key:** lebt im Agent-Speicher hinter `0600`-Socket. Nicht in Config, History, Env-Dateien.
- **Agent-Socket:** `0600`, Owner-only → ersetzt auth-losen `bw serve`-Port.
- **`bw serve` (vom Agent gestartet):** bind nur `127.0.0.1`, ephemerer Port, Teardown per `defer`.
- **Prozess-Env-Sichtbarkeit:** Env-Vars sind unter `/proc/<pid>/environ` für den Owner (und root)
  lesbar — fundamentale Grenze *jeder* Env-Injection (Teller, bws, direnv identisch). Wird ehrlich
  dokumentiert. `syscall.Exec` vermeidet den bwenv-Zwischenprozess.
- **Core Dumps:** `RLIMIT_CORE=0` in Agent und Runner, bevor Secrets im Speicher liegen.
- **Redaction:** zentraler Layer ersetzt bekannte Secret-Werte in *allen* Ausgaben/Fehlern durch `***`.
- **Trust:** Auto-Load nur für per `bwenv allow` freigegebene Verzeichnisse.
- **Touch-ID-Credential:** ThisDeviceOnly, nicht iCloud-gesynct, Secure Enclave.
- **Go-Realität:** Speicher-Zeroization ist mit Go-GC nicht garantiert — dokumentierte Limitierung.

---

## 8. Roadmap (Meilensteine, Aufwand, Abnahme)

| Phase | Inhalt | Aufwand | Abnahmekriterium |
|---|---|---|---|
| **0 Scaffold** | Go-Modul, cobra-Skelett, CI (build/vet/`golangci-lint`/test), goreleaser | 2–3 h | `bwenv --help` läuft, CI grün |
| **1 Provider** | `bw serve`-Lifecycle + Session-Unlock + ein Item holen | ~1 Tag | `bwenv show` listet 1 Item maskiert gg. echte Vaultwarden |
| **2 Config+Resolver** | YAML-Parser + Mapping (name/id/field + bulk folder) | 0,5–1 Tag | Config mit 3 Mappings → korrekte EnvMap (seeded Vault) |
| **3 run** | Env-Injection, Runner, Signal-Forwarding, Exit-Code | ~1 Tag | `bwenv run -- printenv` zeigt Vars; Exit-Code & Ctrl-C durchgereicht |
| **4 Agent** | Unix-Socket, Session-Hold, `unlock`/`lock`, Auto-Lock-TTL | 1,5–2 Tage | 2 Terminals laden ohne erneutes Unlock; `lock` → Hook lädt nichts |
| **5 Shell-Integration** | `hook zsh`, chpwd load/unload, `global`, `allow`/Trust, Failsafe-Timeout | 1–1,5 Tage | Auto-Load im erlaubten Dir; offline/locked → Shell startet sauber |
| **6 Output+Redaction** | `sh`/`export`-Format, `show`-Masking, Fehler-Redaction | 0,5 Tag | `eval "$(bwenv sh)"` funktioniert; kein Klartext in show/Fehlern |
| **7 Touch ID** | Keychain-Enrollment, biometrischer Unlock, Fallback, Build-Tags | ~1 Tag | `--enroll-touchid` → danach Unlock per Touch-ID-Tap; Linux-Build unberührt |
| **8 Hardening** | Socket-`0600`, `RLIMIT_CORE=0`, `syscall.Exec`, ephemerer Port | 0,5–1 Tag | Security-Checkliste besteht; kein Secret in `ps`/History/Logs |
| **9 MVP-Release** | README, goreleaser-Release, Integrationstest gg. dockerisierte Vaultwarden | ~1 Tag | `go install`-bares Binary; CI-Integrationstest grün |

**MVP-Summe: ~8–10 Personentage** (Agent, Shell-Integration und Touch ID sind der Aufpreis
ggü. der schlanken Variante).

**Spätere Phasen (nicht MVP):** `redact`, `scan`, Multi-Provider (weitere Backends), Caching-TTL,
**Variante-B-Provider** (native Krypto).

---

## 9. Risiken & Mitigation

| Risiko | Mitigation |
|---|---|
| `bw`-CLI Breaking Changes / Version-Drift | Beim Start `bw --version` prüfen + `serve`-Feature-Detection; getestete Version in README pinnen |
| Vaultwarden ≠ bitwarden.com API | Integrationstests laufen **gegen Vaultwarden**, nicht nur Cloud |
| `bw serve` ohne Auth | Agent kapselt es hinter `0600`-Socket; ephemerer Port, kurze Lebensdauer |
| Session-Expiry mitten im `run` | 401 erkennen, einmal re-unlock-Prompt, sonst klarer Fehler |
| Shell-Hook blockiert Terminal | Harter Timeout (300 ms) + exit 0 bei jedem Fehler |
| Fremde `bwenv.yaml` löst Vault-Zugriff aus | Trust-Allowlist (`bwenv allow`) |
| Touch-ID-Credential kompromittiert | ThisDeviceOnly + Secure Enclave + biometryCurrentSet (invalidiert bei Biometrie-Änderung) |
| Variante-B-Krypto-Fehler (später) | Gegen Vaultwarden-Testvektoren + Cross-Check mit `bw`-Output verifizieren, bevor Default |

---

## 10. Testkonzept (ohne echte Vault-Daten in CI)

- **Unit:** Provider-Interface mocken → Resolver/Runner/Formatter/Agent-Protokoll komplett ohne
  echten Vault testbar. Golden-Files für `sh`/`show`-Ausgabe.
- **Integration:** GitHub-Actions-**Service-Container** mit `vaultwarden/server`. Seed-Skript legt
  Test-Account + Items an (via `bw` gegen die lokale Instanz). Tests fahren echten
  `login → unlock → run`-Flow. Keine echten Secrets, alles ephemer.
- **Failsafe-Tests:** Server offline / Agent aus / locked → `bwenv export` gibt nichts aus, exit 0,
  innerhalb Timeout.
- **Touch ID:** nicht in CI automatisierbar (Hardware) → manuelle Abnahme + gemocktes
  Keychain-Interface für Unit-Tests der Enrollment-Logik.

---

## 11. Offene Punkte für die Umsetzung

- Endgültiger Tool-Name (`bwenv` bestätigt).
- Lizenz (MIT/Apache-2.0?) — aktuell TBD.
- `syscall.Exec` vs. `exec.Command` als Default im Runner (Signal-Handling-Abwägung).
- Idle-TTL-Default des Agents (Startwert 15 min) — konfigurierbar.
- Go-Keychain-Bibliothek für biometrie-gated Zugriff (`keybase/go-keychain` vs. eigener cgo-Wrapper).
