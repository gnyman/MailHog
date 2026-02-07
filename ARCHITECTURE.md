# Architecture

MailHog is an email testing tool that captures SMTP messages and displays them in a web UI. It runs as a single binary with three subsystems: an SMTP server, a REST API, and a web UI.

## Repository Structure

This is a monorepo. The main binary lives at the root, and the two core packages are embedded as subdirectories:

```
MailHog/
├── main.go                  # Entry point, wires everything together
├── config/                  # Common config (auth, web path)
├── MailHog-Server/          # SMTP server + API backend
│   ├── smtp/                #   SMTP listener and session handler
│   ├── api/                 #   REST API (v1 + v2)
│   ├── config/              #   Server config (ports, storage, release settings)
│   └── websockets/          #   WebSocket hub for live updates
├── MailHog-UI/              # Web frontend
│   ├── assets/              #   Static files (JS, CSS, templates, images)
│   │   └── assets.go        #   Compiled binary blob (go-bindata)
│   ├── web/                 #   Template rendering + route setup
│   └── config/              #   UI config (bind address, API host)
├── vendor/                  # Go module vendor directory
└── .github/workflows/       # CI/CD
```

## How It Starts

`main.go` does three things:

1. **Registers and parses flags** from all three config packages (common, server, UI)
2. **Starts HTTP listener(s)** — if the API and UI share the same bind address (default), they share one listener; otherwise two separate ones
3. **Starts the SMTP listener** in a goroutine

```
main.go
  ├── configure()
  │     ├── cfgcom.RegisterFlags()   # auth-file, web-path
  │     ├── cfgapi.RegisterFlags()   # smtp-bind-addr, api-bind-addr, hostname, storage, release-smtp-addr, ...
  │     └── cfgui.RegisterFlags()    # ui-bind-addr, api-host
  │
  ├── http.Listen()                  # Starts HTTP (API + UI on same or separate ports)
  │     ├── web.CreateWeb()          #   Mounts UI routes (serves assets + templates)
  │     └── api.CreateAPI()          #   Mounts API routes (v1 + v2)
  │
  └── smtp.Listen()                  # Starts SMTP on :1025
```

## Message Flow

```
Sender (e.g. your app)
  │
  ▼
SMTP Server (:1025)
  │  Accepts connection, parses SMTP commands
  │  Stores parsed message in storage
  │  Sends message to MessageChan
  │
  ├──► Storage (in-memory / MongoDB / maildir)
  │
  └──► MessageChan
        │
        ├──► API v1 EventStream (SSE)
        └──► API v2 WebSocket hub
              │
              ▼
           Web UI (live update)
```

## Key Data Types (mailhog/data)

- **`SMTPMessage`** — raw envelope: `From`, `To []string`, `Helo`, `Data` (the original SMTP transaction)
- **`Message`** — parsed message: `ID`, `From *Path`, `To []*Path`, `Content *Content`, `Raw *SMTPMessage`
- **`Content`** — headers + body, with optional MIME parts

The `Raw` field preserves the original SMTP envelope, which is used during message release.

## API Endpoints

### v1 (stable)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/messages` | List messages |
| GET | `/api/v1/messages/{id}` | Get message |
| DELETE | `/api/v1/messages/{id}` | Delete message |
| DELETE | `/api/v1/messages` | Delete all |
| GET | `/api/v1/messages/{id}/download` | Download .eml |
| GET | `/api/v1/messages/{id}/mime/part/{n}/download` | Download MIME part |
| POST | `/api/v1/messages/{id}/release` | Release message |
| GET | `/api/v1/events` | SSE event stream |

### v2 (experimental)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v2/messages` | Paginated message list |
| GET | `/api/v2/search` | Search (by from/to/containing) |
| GET | `/api/v2/websocket` | WebSocket for live updates |

## Release Workflow

When you click "Release" in the UI, the message is sent to a real SMTP server using the **original envelope** data:

- **MAIL FROM**: original sender (`msg.Raw.From`)
- **RCPT TO**: original recipients (`msg.Raw.To`)
- **EHLO**: original HELO hostname (`msg.Raw.Helo`)
- **Message body**: reconstructed from stored headers + body

The target SMTP server defaults to `localhost:25` and is configured via:
- `-release-smtp-addr` flag or `MH_RELEASE_SMTP_ADDR` env var
- `-release-starttls` flag or `MH_RELEASE_STARTTLS` env var (off by default)

## UI Assets

The UI is an AngularJS 1.3 app. Source files live in `MailHog-UI/assets/` (templates, JS, CSS). These are compiled into `assets/assets.go` using `go-bindata`, which embeds them in the binary.

To regenerate after editing UI files:
```bash
go-bindata -pkg assets -o MailHog-UI/assets/assets.go MailHog-UI/assets/...
```

## Building

```bash
go build -o MailHog .
```

Cross-compile (static, no CGO):
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o MailHog_linux_amd64 .
```

## Go Module Setup

The root `go.mod` uses `replace` directives to point to the local subdirectories:
```
replace (
    github.com/gnyman/MailHog-Server => ./MailHog-Server
    github.com/gnyman/MailHog-UI => ./MailHog-UI
)
```

Upstream `mailhog/*` packages (data, smtp, storage, http) are pulled from GitHub as regular dependencies.
