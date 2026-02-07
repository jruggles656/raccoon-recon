package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jamesruggles/reconsuite/internal/database"
	"github.com/jamesruggles/reconsuite/internal/tools"
)

// Broadcaster sends output lines to connected WebSocket clients.
type Broadcaster interface {
	Broadcast(scanID int64, line tools.OutputLine)
}

// Executor orchestrates scan lifecycle.
type Executor struct {
	db          *database.DB
	broadcaster Broadcaster
	mu          sync.Mutex
	cancels     map[int64]context.CancelFunc
}

func NewExecutor(db *database.DB, broadcaster Broadcaster) *Executor {
	return &Executor{
		db:          db,
		broadcaster: broadcaster,
		cancels:     make(map[int64]context.CancelFunc),
	}
}

// StartScan creates a scan record and begins execution in a goroutine.
func (e *Executor) StartScan(scan *database.Scan) error {
	scan.Status = "pending"
	if scan.Parameters == "" {
		scan.Parameters = "{}"
	}
	if err := e.db.CreateScan(scan); err != nil {
		return fmt.Errorf("create scan: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancels[scan.ID] = cancel
	e.mu.Unlock()

	go e.runScan(ctx, scan)
	return nil
}

// CancelScan cancels a running scan.
func (e *Executor) CancelScan(scanID int64) {
	e.mu.Lock()
	cancel, ok := e.cancels[scanID]
	e.mu.Unlock()
	if ok {
		cancel()
	}
}

var builtinTools = map[string]bool{
	"google_dorking":   true,
	"osint_aggregator": true,
	"ssl_check":        true,
	"robots_sitemap":   true,
	"metadata_extract": true,
}

func (e *Executor) runScan(ctx context.Context, scan *database.Scan) {
	defer func() {
		e.mu.Lock()
		delete(e.cancels, scan.ID)
		e.mu.Unlock()
	}()

	// Route built-in tools to their own handler
	if builtinTools[scan.Tool] {
		e.runBuiltinScan(ctx, scan)
		return
	}

	spec, err := e.buildToolSpec(scan)
	if err != nil {
		slog.Error("build tool spec failed", "scan_id", scan.ID, "error", err)
		e.db.UpdateScanStatus(scan.ID, "failed")
		e.broadcaster.Broadcast(scan.ID, tools.OutputLine{
			Timestamp: time.Now(), Stream: "stderr", Line: "Error: " + err.Error(),
		})
		e.broadcaster.Broadcast(scan.ID, tools.OutputLine{Done: true})
		return
	}

	e.db.UpdateScanStatus(scan.ID, "running")

	outputCh := make(chan tools.OutputLine, 100)

	var result *tools.ToolResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = tools.Run(ctx, spec, outputCh)
	}()

	// Stream output to broadcaster and accumulate raw output
	var rawOutput strings.Builder
	for line := range outputCh {
		e.broadcaster.Broadcast(scan.ID, line)
		rawOutput.WriteString(line.Line)
		rawOutput.WriteByte('\n')
	}

	wg.Wait()

	// Store raw output
	e.db.UpdateScanRawOutput(scan.ID, rawOutput.String())

	if result.Error != nil && ctx.Err() != nil {
		e.db.UpdateScanStatus(scan.ID, "failed")
		e.broadcaster.Broadcast(scan.ID, tools.OutputLine{
			Timestamp: time.Now(), Stream: "stderr", Line: "Scan cancelled",
		})
	} else if result.Error != nil {
		e.db.UpdateScanStatus(scan.ID, "failed")
	} else {
		// Parse results
		results := e.parseResults(scan, result)
		if len(results) > 0 {
			if err := e.db.CreateResults(results); err != nil {
				slog.Error("store results failed", "scan_id", scan.ID, "error", err)
			}
		}
		e.db.UpdateScanStatus(scan.ID, "completed")
	}

	e.broadcaster.Broadcast(scan.ID, tools.OutputLine{Done: true, Timestamp: time.Now()})
}

func (e *Executor) buildToolSpec(scan *database.Scan) (tools.ToolSpec, error) {
	var params map[string]string
	if scan.Parameters != "" && scan.Parameters != "{}" {
		json.Unmarshal([]byte(scan.Parameters), &params)
	}
	if params == nil {
		params = make(map[string]string)
	}

	switch scan.Tool {
	case "whois":
		return buildWhoisSpec(scan.Target)
	case "dig":
		return buildDigSpec(scan.Target, params["record_type"])
	case "theharvester":
		return buildTheHarvesterSpec(scan.Target, params["sources"])
	case "dnsrecon":
		return buildDnsReconSpec(scan.Target, params["scan_mode"])
	case "nmap":
		return buildNmapSpec(scan.Target, params)
	case "traceroute":
		return buildTracerouteSpec(scan.Target)
	case "snmpwalk":
		return buildSnmpWalkSpec(scan.Target, params["community"], params["oid"])
	case "netcat":
		return buildNetcatSpec(scan.Target, params["port"])
	case "curl":
		return buildCurlSpec(scan.Target)
	case "whatweb":
		return buildWhatWebSpec(scan.Target, params["aggression"])
	case "gobuster":
		return buildGobusterSpec(scan.Target, params["wordlist"], params["extensions"])
	case "google_dorking":
		return tools.ToolSpec{Name: "Google Dorking", BinaryName: "__builtin__"}, nil
	case "osint_aggregator":
		return tools.ToolSpec{Name: "OSINT Aggregator", BinaryName: "__builtin__"}, nil
	case "ssl_check":
		return tools.ToolSpec{Name: "SSL/TLS Check", BinaryName: "__builtin__"}, nil
	case "robots_sitemap":
		return tools.ToolSpec{Name: "Robots/Sitemap", BinaryName: "__builtin__"}, nil
	case "metadata_extract":
		return tools.ToolSpec{Name: "Metadata Extractor", BinaryName: "__builtin__"}, nil
	default:
		return tools.ToolSpec{}, fmt.Errorf("unknown tool: %s", scan.Tool)
	}
}

func (e *Executor) parseResults(scan *database.Scan, result *tools.ToolResult) []database.Result {
	switch scan.Tool {
	case "whois":
		return parseWhoisResults(scan.ID, result.Stdout)
	case "dig":
		return parseDigResults(scan.ID, result.Stdout)
	case "nmap":
		return parseNmapResults(scan.ID, result.Stdout)
	case "curl":
		return parseCurlResults(scan.ID, result.Stdout)
	default:
		// For tools without a dedicated parser, store raw output as a single result
		if result.Stdout != "" {
			return []database.Result{{
				ScanID:     scan.ID,
				ResultType: "raw",
				Key:        scan.Tool,
				Value:      result.Stdout,
			}}
		}
		return nil
	}
}
