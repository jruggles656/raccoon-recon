# ReconSuite

A web-based reconnaissance tool for penetration testing, built in Go. Provides a clean dark-themed dashboard for conducting passive, active, and web reconnaissance during the information-gathering phase of a pentest.

Built for IST-4620 Penetration Testing & Ethical Hacking (Spring 2026).

## Features

### Passive Reconnaissance
- **WHOIS Lookup** — Domain registration, registrar, nameservers
- **DNS Records** — A, AAAA, MX, NS, TXT, SOA, CNAME via `dig`
- **Subdomain Enumeration** — Discover subdomains via `theHarvester`
- **DNS Recon** — Standard enumeration, reverse DNS, zone transfers via `dnsrecon`
- **Google Dorking** — Auto-generated Google dork queries for target
- **OSINT Aggregator** — Links to Shodan, Censys, VirusTotal, crt.sh, and more

### Active Reconnaissance
- **Port Scanning** — TCP connect scans via `nmap`
- **Service Detection** — Version detection with `nmap -sV`
- **OS Fingerprinting** — OS detection with `nmap -O`
- **Ping Sweep** — Live host discovery with `nmap -sn`
- **Banner Grabbing** — Service banners via `nmap` or `netcat`
- **Traceroute** — Network path discovery
- **SNMP Enumeration** — SNMP tree walking via `snmpwalk`

### Web Reconnaissance
- **HTTP Header Analysis** — Response headers via `curl`
- **Technology Detection** — CMS/framework identification via `whatweb`
- **Directory Discovery** — Brute-force directories via `gobuster`
- **SSL/TLS Analysis** — Certificate details, cipher suites, TLS version (built-in Go)
- **Robots.txt / Sitemap** — Fetch and parse (built-in Go)

### Other
- **Project Management** — Organize scans by engagement
- **Real-time Output** — Live scan output streamed via WebSocket
- **SQLite Database** — All results persisted for historical review
- **Report Generation** — Export findings as Markdown or PDF
- **Dark Dashboard** — Professional cybersecurity-themed UI

## Prerequisites

### macOS (Homebrew)
```bash
brew install go nmap whatweb gobuster
pip3 install theHarvester dnsrecon
```

### Debian/Ubuntu
```bash
sudo apt install golang nmap whatweb gobuster whois dnsutils traceroute curl netcat-openbsd snmp
pip3 install theHarvester dnsrecon
```

Not all tools are required. ReconSuite detects which tools are available and disables features for missing tools.

## Build & Run

```bash
# Build
go build -o reconsuite .

# Run (serves on http://localhost:8080)
./reconsuite

# Run with custom config
./reconsuite --config myconfig.yaml
```

Open your browser to **http://localhost:8080**.

## Configuration

Edit `config.yaml`:

```yaml
server:
  host: "127.0.0.1"
  port: 8080

database:
  path: "reconsuite.db"

reports:
  directory: "./reports"
```

## Project Structure

```
reconsuite/
├── main.go                      # Entry point
├── config.yaml                  # Configuration
├── internal/
│   ├── config/config.go         # Config loading
│   ├── database/                # SQLite schema, models, queries
│   ├── scanner/                 # Scan orchestration, tool specs, parsers
│   ├── server/                  # HTTP server, handlers, WebSocket, middleware
│   ├── tools/                   # Tool wrappers, validation, detection
│   └── report/                  # Markdown + PDF report generation
├── web/
│   ├── embed.go                 # Embedded static files
│   ├── templates/               # HTML templates
│   └── static/                  # CSS, JS
└── reports/                     # Generated report output
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects` | List projects |
| POST | `/api/projects` | Create project |
| GET | `/api/projects/{id}` | Get project |
| PUT | `/api/projects/{id}` | Update project |
| DELETE | `/api/projects/{id}` | Delete project |
| POST | `/api/scans` | Start a scan |
| GET | `/api/scans/{id}` | Get scan status |
| DELETE | `/api/scans/{id}` | Cancel scan |
| GET | `/api/scans/{id}/results` | Get scan results |
| POST | `/api/reports` | Generate report |
| GET | `/api/reports/{id}/download` | Download report |
| GET | `/api/tools/status` | Check installed tools |
| GET | `/api/stats` | Dashboard statistics |
| WS | `/ws` | WebSocket for live scan output |

## Tech Stack

- **Go** standard library for HTTP server
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGO)
- **WebSocket** via `github.com/coder/websocket`
- **PDF** via `github.com/signintech/gopdf`
- **Config** via `gopkg.in/yaml.v3`

## License

MIT
