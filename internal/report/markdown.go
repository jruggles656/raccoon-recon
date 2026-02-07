package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesruggles/reconsuite/internal/database"
)

type Generator struct {
	db         *database.DB
	reportsDir string
}

func NewGenerator(db *database.DB, reportsDir string) *Generator {
	return &Generator{db: db, reportsDir: reportsDir}
}

func (g *Generator) GenerateMarkdown(projectID int64) (string, error) {
	project, err := g.db.GetProject(projectID)
	if err != nil || project == nil {
		return "", fmt.Errorf("project not found")
	}

	scans, err := g.db.ListScansByProject(projectID)
	if err != nil {
		return "", fmt.Errorf("listing scans: %w", err)
	}

	results, err := g.db.GetResultsByProject(projectID)
	if err != nil {
		return "", fmt.Errorf("listing results: %w", err)
	}

	var b strings.Builder

	// Title
	b.WriteString(fmt.Sprintf("# Reconnaissance Report: %s\n\n", project.Name))
	b.WriteString(fmt.Sprintf("**Generated:** %s  \n", time.Now().Format("January 2, 2006 15:04:05 MST")))
	b.WriteString(fmt.Sprintf("**Tool:** ReconSuite  \n\n"))

	// Scope
	b.WriteString("## Scope\n\n")
	if project.Scope != "" {
		for _, target := range strings.Split(project.Scope, "\n") {
			target = strings.TrimSpace(target)
			if target != "" {
				b.WriteString(fmt.Sprintf("- `%s`\n", target))
			}
		}
	} else {
		b.WriteString("No scope defined.\n")
	}
	b.WriteString("\n")

	// Executive Summary
	b.WriteString("## Executive Summary\n\n")
	b.WriteString(fmt.Sprintf("This report covers %d scan(s) performed against the defined scope. ", len(scans)))
	b.WriteString(fmt.Sprintf("A total of %d finding(s) were recorded.\n\n", len(results)))

	// Count by type
	typeCounts := make(map[string]int)
	for _, r := range results {
		typeCounts[r.ResultType]++
	}
	if len(typeCounts) > 0 {
		b.WriteString("| Finding Type | Count |\n")
		b.WriteString("|---|---|\n")
		for t, c := range typeCounts {
			b.WriteString(fmt.Sprintf("| %s | %d |\n", t, c))
		}
		b.WriteString("\n")
	}

	// Methodology
	b.WriteString("## Methodology\n\n")
	b.WriteString("The following tools were used during reconnaissance:\n\n")
	toolSet := make(map[string]bool)
	for _, s := range scans {
		toolSet[s.Tool] = true
	}
	for tool := range toolSet {
		b.WriteString(fmt.Sprintf("- %s\n", tool))
	}
	b.WriteString("\n")

	// Findings grouped by scan type
	scansByType := map[string][]database.Scan{
		"passive": {},
		"active":  {},
		"web":     {},
	}
	for _, s := range scans {
		scansByType[s.ScanType] = append(scansByType[s.ScanType], s)
	}

	sections := []struct {
		title    string
		scanType string
	}{
		{"Passive Reconnaissance Findings", "passive"},
		{"Active Reconnaissance Findings", "active"},
		{"Web Reconnaissance Findings", "web"},
	}

	for _, sec := range sections {
		sectionScans := scansByType[sec.scanType]
		if len(sectionScans) == 0 {
			continue
		}

		b.WriteString(fmt.Sprintf("## %s\n\n", sec.title))

		for _, scan := range sectionScans {
			scanResults, _ := g.db.GetResultsByScan(scan.ID)

			b.WriteString(fmt.Sprintf("### %s — %s\n\n", scan.Tool, scan.Target))
			b.WriteString(fmt.Sprintf("**Status:** %s  \n", scan.Status))
			if scan.StartedAt != nil {
				b.WriteString(fmt.Sprintf("**Started:** %s  \n", scan.StartedAt.Format(time.RFC3339)))
			}
			b.WriteString("\n")

			if len(scanResults) > 0 {
				b.WriteString("| Type | Key | Value |\n")
				b.WriteString("|---|---|---|\n")
				for _, r := range scanResults {
					val := r.Value
					if len(val) > 100 {
						val = val[:100] + "..."
					}
					b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.ResultType, r.Key, val))
				}
				b.WriteString("\n")
			} else {
				b.WriteString("No structured results for this scan.\n\n")
			}
		}
	}

	// Raw Output Appendix
	b.WriteString("## Appendix: Raw Tool Output\n\n")
	for _, scan := range scans {
		if scan.RawOutput == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s — %s\n\n", scan.Tool, scan.Target))
		b.WriteString("```\n")
		output := scan.RawOutput
		if len(output) > 5000 {
			output = output[:5000] + "\n... (truncated)"
		}
		b.WriteString(output)
		b.WriteString("\n```\n\n")
	}

	return b.String(), nil
}

func (g *Generator) SaveMarkdown(projectID int64) (string, *database.Report, error) {
	content, err := g.GenerateMarkdown(projectID)
	if err != nil {
		return "", nil, err
	}

	project, _ := g.db.GetProject(projectID)
	name := "report"
	if project != nil {
		name = strings.ReplaceAll(strings.ToLower(project.Name), " ", "-")
	}

	os.MkdirAll(g.reportsDir, 0755)
	filename := fmt.Sprintf("%s-%s.md", name, time.Now().Format("20060102-150405"))
	path := filepath.Join(g.reportsDir, filename)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", nil, fmt.Errorf("writing report: %w", err)
	}

	rpt := &database.Report{
		ProjectID: projectID,
		Title:     fmt.Sprintf("Recon Report — %s", name),
		Format:    "markdown",
		Content:   content,
		FilePath:  path,
	}
	if err := g.db.CreateReport(rpt); err != nil {
		return "", nil, fmt.Errorf("saving report record: %w", err)
	}

	return path, rpt, nil
}
