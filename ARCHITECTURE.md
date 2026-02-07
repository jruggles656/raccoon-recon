# Raccoon Recon — Architecture

This document describes how the application works from startup to shutdown, covering every package, data flow, and design decision.

---

## 1. High-Level Overview

Raccoon Recon is a single-binary Go web app. On startup it:

1. Loads config from `config.yaml` (or uses defaults)
2. Opens a SQLite database and runs schema migrations
3. Starts an HTTP server that serves HTML pages, a REST API, and a WebSocket endpoint

All HTML templates, CSS, JS, and images are **embedded** into the binary via Go's `embed.FS`, so there are no external file dependencies at runtime.

```
┌──────────────────────────────────────────────────────┐
│                     Browser                          │
│  (dashboard, passive, active, web, results, reports) │
└─────────────┬────────────────────────┬───────────────┘
              │ HTTP / REST            │ WebSocket
              ▼                        ▼
┌─────────────────────────────────────────────────────┐
│                 internal/server                      │
│  server.go  handlers.go  websocket.go  middleware.go │
└──────┬──────────────┬──────────────┬────────────────┘
       │              │              │
       ▼              ▼              ▼
 internal/scanner  internal/tools  internal/database
 (executor, specs, (run CLI tools, (SQLite via
  builtin, parsers, validate input, modernc.org/sqlite)
  filemeta)         detect installs)
       │
       ▼
 internal/report
 (markdown.go, pdf.go)
```

---

## 2. Startup Flow (`main.go`)

```
main()
  ├─ flag.Parse()              // --config flag (default: config.yaml)
  ├─ config.Load(path)         // read YAML or use defaults
  ├─ database.New(dsn)         // open SQLite, enable WAL + FK, run migrations
  ├─ server.New(cfg, db)       // create hub, executor, report generator, load templates, register routes
  └─ srv.ListenAndServe()      // wrap mux in middleware chain, bind to host:port
```

The middleware chain is applied in this order (outermost first):
1. **recoveryMiddleware** — catches panics, returns 500
2. **securityHeaders** — adds X-Content-Type-Options, X-Frame-Options, X-XSS-Protection
3. **loggingMiddleware** — logs method, path, status, duration via `slog`

---

## 3. Package-by-Package Breakdown

### 3.1 `internal/config` — Configuration

**File:** `config.go`

Loads a YAML file with three sections: `server` (host + port), `database` (file path), `reports` (output directory). If the file doesn't exist, it silently uses defaults:

| Setting | Default |
|---------|---------|
| `server.host` | `127.0.0.1` |
| `server.port` | `8080` |
| `database.path` | `reconsuite.db` |
| `reports.directory` | `./reports` |

### 3.2 `internal/database` — SQLite Persistence

**Files:** `db.go`, `models.go`, `migrations.go`, `queries.go`

#### Connection (`db.go`)
- Opens SQLite via `modernc.org/sqlite` (pure Go, no CGO required)
- `MaxOpenConns(1)` — SQLite is single-writer, prevents BUSY errors
- Enables **WAL** (Write-Ahead Logging) for concurrent reads
- Enables **foreign keys** enforcement
- Runs schema migration on every startup (`CREATE TABLE IF NOT EXISTS`)

#### Schema (`migrations.go`)
Four tables with indexes:

```
projects
  ├── id (PK, autoincrement)
  ├── name, description, scope
  └── created_at, updated_at

scans
  ├── id (PK, autoincrement)
  ├── project_id (FK → projects, nullable for quick scans)
  ├── scan_type (passive | active | web)
  ├── tool, target, parameters (JSON string)
  ├── status (pending | running | completed | failed)
  ├── raw_output (full CLI output text)
  └── started_at, completed_at, created_at

results
  ├── id (PK, autoincrement)
  ├── scan_id (FK → scans)
  ├── result_type, key, value, details
  └── created_at

reports
  ├── id (PK, autoincrement)
  ├── project_id (FK → projects)
  ├── title, format (markdown | pdf), content, file_path
  └── created_at
```

Indexes: `idx_scans_project`, `idx_scans_status`, `idx_results_scan`, `idx_results_type`, `idx_reports_project`

#### Models (`models.go`)
Go structs with JSON tags: `Project`, `Scan`, `Result`, `Report`.

`Scan.ProjectID` is stored as `int64` in the struct, but inserted as `NULL` when value is `0` (for dashboard quick scans that aren't tied to a project). Read back via `sql.NullInt64`.

#### Queries (`queries.go`)
CRUD functions for all four tables, plus:
- `GetStats()` — counts for dashboard cards
- `ListRecentScans(limit)` — last N scans across all projects
- `CreateResults([]Result)` — batch insert inside a transaction

### 3.3 `internal/server` — HTTP Server

**Files:** `server.go`, `handlers.go`, `websocket.go`, `middleware.go`

#### Server struct (`server.go`)
Holds references to config, database, WebSocket hub, scan executor, report generator, HTTP mux, and pre-compiled template map.

Templates are parsed at startup from `embed.FS`: each page template is combined with `layout.html` to form a complete HTML page.

#### Routes (`server.go` → `registerRoutes`)

| Path | Handler | Purpose |
|------|---------|---------|
| `/` | `handleDashboard` | Dashboard page |
| `/projects` | `handleProjects` | Projects page |
| `/passive` | `handlePassive` | Passive recon page |
| `/active` | `handleActive` | Active recon page |
| `/web` | `handleWeb` | Web recon page |
| `/results` | `handleResults` | Results viewer |
| `/reports` | `handleReports` | Reports page |
| `/static/` | `http.FileServer` | Embedded CSS/JS/images |
| `/api/projects` | `handleAPIProjects` | List/create projects |
| `/api/projects/{id}` | `handleAPIProject` | Get/update/delete project |
| `/api/stats` | `handleAPIStats` | Dashboard counts |
| `/api/scans` | `handleAPIScans` | Start scan (POST) |
| `/api/scans/{id}` | `handleAPIScan` | Get/cancel scan |
| `/api/scans/{id}/results` | (inside handleAPIScan) | Get scan results |
| `/api/scans/recent` | (inside handleAPIScans) | Last 10 scans |
| `/api/reports` | `handleAPIReports` | Generate report (POST) |
| `/api/reports/{id}` | `handleAPIReport` | Download report |
| `/api/tools/status` | `handleAPIToolStatus` | Installed tool check |
| `/api/upload/metadata` | `handleAPIFileMetadata` | File metadata extraction |
| `/ws` | `handleWebSocket` | Live scan output |

#### Handlers (`handlers.go`)
- Page handlers render templates with an `ActivePage` field for sidebar highlighting
- API handlers use method checking (`r.Method`) to route GET/POST/PUT/DELETE
- `handleAPIScans` POST: creates a `database.Scan` from JSON body, calls `executor.StartScan()`
- `handleAPIFileMetadata` POST: parses multipart form (10MB limit), reads file bytes, calls `scanner.ExtractFileMetadata()`, returns JSON
- Helper: `writeJSON()` and `writeError()` for consistent API responses

#### WebSocket (`websocket.go`)
The `Hub` manages a map of `scanID → set of *websocket.Conn`. Flow:

1. Client opens WebSocket to `/ws`
2. Client sends `{ "scan_id": 123 }`
3. Server calls `hub.Subscribe(scanID, conn)`
4. **Race condition check**: immediately queries the DB — if the scan already completed, sends `{ "done": true }` and returns
5. Otherwise, holds connection open; `hub.Broadcast()` pushes output lines as they arrive
6. When scan finishes, executor broadcasts `{ "done": true }`

The client also runs a **polling fallback** (every 500ms, up to 30 seconds) in parallel with the WebSocket, using a shared `finished` flag to prevent double-handling. This ensures results are always captured even if the scan completes before the WebSocket subscribes.

#### Middleware (`middleware.go`)
Three middleware layers applied around the mux:
- **Recovery** — `recover()` from panics, log error, return 500
- **Security headers** — `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `X-XSS-Protection: 1; mode=block`
- **Logging** — structured log via `slog` (method, path, status code, duration)

Uses a custom `responseWriter` wrapper to capture the status code.

### 3.4 `internal/scanner` — Scan Orchestration

**Files:** `executor.go`, `specs.go`, `builtin.go`, `parsers.go`, `filemeta.go`

#### Executor (`executor.go`)
The core scan lifecycle manager. Key concepts:

- **Broadcaster interface** — the WebSocket hub implements this to receive output lines
- **cancels map** — tracks `context.CancelFunc` per scan ID for cancellation support

**Scan flow:**
```
StartScan(scan)
  ├─ Set status = "pending", save to DB
  ├─ Create context with cancel
  └─ Launch goroutine: runScan(ctx, scan)

runScan(ctx, scan)
  ├─ Is it a built-in tool? → runBuiltinScan() (see below)
  ├─ Otherwise:
  │   ├─ buildToolSpec(scan) → creates ToolSpec with binary name, args, timeout
  │   ├─ Update status = "running"
  │   ├─ Launch goroutine: tools.Run(ctx, spec, outputCh)
  │   ├─ Read from outputCh, broadcast each line, accumulate raw output
  │   ├─ Wait for tool to finish
  │   ├─ Save raw output to DB
  │   ├─ Parse results via parseResults() → save structured results to DB
  │   └─ Update status = "completed" or "failed"
  └─ Broadcast { done: true }
```

#### Tool Specifications (`specs.go`)
Each CLI tool has a `build*Spec()` function that:
1. Validates the target (via `tools.ValidateTarget` or `tools.ValidateURL`)
2. Builds the argument list
3. Sets a timeout
4. Returns a `tools.ToolSpec`

Supported tools: `whois`, `dig`, `theharvester`, `dnsrecon`, `nmap`, `traceroute`, `snmpwalk`, `netcat` (nc), `curl`, `whatweb`, `gobuster`

Nmap supports multiple scan types via a `scan_type` parameter: default (TCP connect), service detection (`-sV`), OS fingerprinting (`-O`), ping sweep (`-sn`), banner grabbing (`--script=banner`).

#### Built-in Tools (`builtin.go`)
Five tools that don't need external binaries:

| Tool | What it does |
|------|-------------|
| `google_dorking` | Generates 10 Google dork URLs targeting the domain (files, logins, sensitive data, subdomains, errors) |
| `osint_aggregator` | Generates links to VirusTotal, crt.sh, SecurityTrails, Shodan, Censys, etc. Detects IP vs domain for appropriate links |
| `ssl_check` | Connects via TLS, extracts version, cipher suite, certificate subject/issuer/dates/SANs |
| `robots_sitemap` | Fetches `/robots.txt` and `/sitemap.xml`, parses disallowed paths |
| `metadata_extract` | Fetches a URL, extracts HTTP headers, `<title>`, `<meta>` tags, Open Graph data, canonical URL, favicon |

HTML parsing is done with simple string operations (no external HTML parser). The `extractHTMLTag`, `parseMetaTags`, `extractAttr`, and `extractLinkRel` functions handle extraction.

#### Output Parsers (`parsers.go`)
Parse raw CLI output into structured `database.Result` records:

| Parser | How it works |
|--------|-------------|
| `parseWhoisResults` | Looks for known field prefixes (Registrar, Creation Date, etc.) |
| `parseDigResults` | Splits answer lines into fields (name, TTL, class, type, value) |
| `parseNmapResults` | XML unmarshaling (`-oX -` flag) into typed structs for ports, services, OS matches |
| `parseCurlResults` | Splits `Header: Value` lines, captures HTTP status |

For tools without a dedicated parser, raw stdout is stored as a single result.

#### File Metadata Extraction (`filemeta.go`)
Standalone file analysis (not part of the scan/executor flow):

**JPEG/EXIF:**
- Finds APP1 marker (`0xFF 0xE1`) in JPEG structure
- Reads TIFF header: byte order (`II` = little-endian, `MM` = big-endian), magic number 42
- Parses IFD0 entries (12 bytes each: tag, type, count, value)
- Follows pointers to Exif sub-IFD (tag `0x8769`) and GPS IFD (tag `0x8825`)
- Extracts: camera make/model, dates, exposure, f-number, ISO, focal length, orientation, software
- GPS: converts DMS rationals to decimal coordinates, links to Google Maps on frontend

**PNG:**
- Dimensions via Go's `image/png`
- Walks PNG chunk structure (length + type + data + CRC)
- Extracts `tEXt` and `iTXt` chunks (keyword + null separator + text)

**PDF:**
- Counts pages by finding `/Type /Page` (excluding `/Type /Pages`)
- Extracts `/Info` dictionary entries: Title, Author, Subject, Creator, Producer, dates
- Handles both parenthesized strings `(text)` and hex strings `<FEFF...>`
- Formats PDF date strings (`D:YYYYMMDD...` → `YYYY-MM-DD HH:MM:SS`)
- Gets PDF version from `%PDF-X.X` header

### 3.5 `internal/tools` — Tool Utilities

**Files:** `common.go`, `validator.go`, `detect.go`

#### Tool Runner (`common.go`)
`Run(ctx, spec, outputCh)`:
- Creates an `exec.CommandContext` with the specified binary and args
- Pipes stdout and stderr separately
- Two goroutines scan stdout/stderr line-by-line, sending `OutputLine` structs to the channel
- Channel is closed when the tool exits
- Returns `ToolResult` with exit code, accumulated stdout/stderr, duration, error

Key types:
- `ToolSpec` — name, binary, args, timeout
- `ToolResult` — exit code, stdout, stderr, duration, error
- `OutputLine` — timestamp, stream (stdout/stderr), line text, done flag

#### Input Validation (`validator.go`)
- `ValidateTarget(target)` — accepts IPs, CIDRs (min /16 for IPv4, /48 for IPv6), and hostnames matching a strict regex. Blocks shell metacharacters (`;|&\`$(){}[]!<>\"'`)
- `ValidateURL(target)` — requires `http://` or `https://` prefix, allows URL-safe characters
- `SanitizeArg(arg)` — strips dangerous characters from a single argument

#### Tool Detection (`detect.go`)
`DetectAll()` checks 10 tools via `exec.LookPath`:
nmap, theHarvester, dnsrecon, whatweb, whois, dig, curl, gobuster, traceroute, nc

For each tool, runs its version command and captures the first line. Results are shown on the dashboard "Tool Status" grid.

### 3.6 `internal/report` — Report Generation

**Files:** `markdown.go`, `pdf.go`

#### Markdown Reports (`markdown.go`)
Generates a structured Markdown document:
- Title with project name, timestamp
- Scope (from project definition)
- Executive summary with finding counts by type
- Methodology (list of tools used)
- Findings grouped by scan type (passive → active → web)
- Each scan: tool name, target, status, results table
- Appendix: raw tool output (truncated at 5000 chars)

Saved to `reports/` directory, recorded in `reports` DB table.

#### PDF Reports (`pdf.go`)
Uses `github.com/signintech/gopdf`:
- Loads Helvetica (macOS) or DejaVuSans (Linux) font
- Custom `pdfWriter` helper manages Y position, page breaks, and text layout
- Title page with centered text
- Same content structure as Markdown: scope, summary, methodology, findings, results tables
- 3-column tables for results (type, key, value)
- Values truncated at 60 chars for PDF table cells
- Automatic page breaks when content exceeds page height

### 3.7 `web/` — Frontend Assets

**Files:** `embed.go`, `templates/*.html`, `static/css/style.css`, `static/js/app.js`, `static/img/logo.svg`

#### Embedding (`embed.go`)
Two embed directives:
- `Templates` — all `templates/*.html` files
- `Static` — entire `static/` directory (CSS, JS, images)

#### Templates (`templates/`)
Go `html/template` with two blocks: `title` and `content`. The layout provides the sidebar navigation and includes `app.js`.

Pages: `dashboard.html`, `projects.html`, `passive.html`, `active.html`, `web.html`, `results.html`, `reports.html`

The dashboard has:
- Stats cards (projects, scans, findings)
- ASCII art banner
- 7 quick-action cards (WHOIS, DNS, Port Scan, SSL/TLS, Metadata, Google Dorking, File Metadata)
- File drop zone for drag-and-drop metadata extraction
- Modal overlay for scan output + results table
- Tool status grid
- Recent scans table

#### JavaScript (`static/js/app.js`)
Key functions:

| Function | Purpose |
|----------|---------|
| `runScan(tool, target, type, params)` | Full-page scan: POST to API, open WebSocket + polling, show results |
| `quickScan(tool, inputId, type)` | Dashboard quick scan: same flow in modal overlay |
| `pollScanStatus(scanId, ...)` | Polls `GET /api/scans/{id}` every 500ms as WebSocket fallback |
| `loadScanResults(scanId)` | Fetch and render structured results in page table |
| `loadQAResults(scanId)` | Fetch and render results in quick-action modal table |
| `uploadFileMetadata(file)` | POST multipart to `/api/upload/metadata`, show preview + results |
| `initDashboard()` | Load stats, tool status, recent scans on dashboard |
| `loadProjects()` | Fetch and render project list |
| `esc(str)` | HTML-escape user content to prevent XSS |

The dual WebSocket + polling pattern uses a shared `finished` flag and `markDone()` callback to ensure only one mechanism triggers the completion UI.

#### CSS (`static/css/style.css`)
Monochrome dark theme using CSS custom properties (`--bg-primary`, `--text-primary`, `--accent`, etc.). Features:
- Sidebar navigation with active state highlighting
- Card grid layout for dashboard
- Terminal-style output display with green/red text
- Data tables with alternating row backgrounds
- Modal overlay for quick action results
- File drop zone with dashed border and drag-over highlight
- Badge styles for scan statuses (running, completed, failed)
- Responsive design adjustments

---

## 4. Data Flow Examples

### Quick Scan from Dashboard
```
User clicks "Run" on WHOIS card
  → quickScan('whois', 'qa-whois-target', 'passive')
  → POST /api/scans { tool: "whois", target: "example.com", scan_type: "passive" }
  → handler creates Scan record (project_id = NULL)
  → executor.StartScan() → goroutine launched
  → Modal opens, WebSocket connects, sends { scan_id: 5 }
  → Polling starts in parallel (every 500ms)
  → executor: buildWhoisSpec() → tools.Run() streams output → hub.Broadcast()
  → WebSocket receives lines → displayed in terminal div
  → Tool finishes → parseWhoisResults() → results saved to DB
  → executor broadcasts { done: true }
  → WebSocket or poll detects completion → loadQAResults(5)
  → Results table populated in modal
```

### File Metadata Upload
```
User drops JPEG onto file drop zone
  → uploadFileMetadata(file)
  → FileReader reads file as data URL → shown in <img> preview
  → FormData POST to /api/upload/metadata
  → handler reads multipart file into memory
  → scanner.ExtractFileMetadata(filename, bytes)
    → DetectContentType → "image/jpeg"
    → extractJPEGMetadata → dimensions + EXIF
    → findEXIFSegment → parseEXIF → parseIFD (IFD0 + Exif sub-IFD + GPS IFD)
    → convertGPSResults → decimal lat/lon
  → JSON response with results array
  → Frontend renders results table, GPS linked to Google Maps
```

### Report Generation
```
User clicks "Generate PDF" for project 3
  → POST /api/reports { project_id: 3, format: "pdf" }
  → handler calls reportGen.SavePDF(3)
  → Fetches project, all scans, all results from DB
  → Builds PDF with gopdf: title page, scope, summary, findings tables
  → Saves to reports/project-name-20260205-143022.pdf
  → Creates report record in DB
  → Returns report metadata as JSON
```

---

## 5. Dependencies

| Package | Purpose | Why chosen |
|---------|---------|-----------|
| `modernc.org/sqlite` | SQLite driver | Pure Go — no CGO, no C compiler needed, single binary |
| `github.com/coder/websocket` | WebSocket server | Lightweight, modern API, context-aware |
| `github.com/signintech/gopdf` | PDF generation | No CGO, supports TTF fonts, A4 layout |
| `gopkg.in/yaml.v3` | YAML config parsing | Standard Go YAML library |
| Go stdlib | Everything else | `net/http`, `html/template`, `embed`, `database/sql`, `image/*`, `crypto/tls`, `encoding/binary`, `encoding/xml` |

---

## 6. Build and Deployment

```bash
go build -o reconsuite .    # Produces single binary (~15MB)
./reconsuite                 # Starts on localhost:8080
./reconsuite --config my.yaml  # Custom config path
```

The binary contains all templates, CSS, JS, and images. No external files are needed except:
- `config.yaml` (optional — defaults work)
- CLI tools for non-built-in scans (nmap, whois, dig, etc.)

SQLite database file (`reconsuite.db`) and reports directory (`./reports/`) are created automatically.
