package scanner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jamesruggles/reconsuite/internal/database"
	"github.com/jamesruggles/reconsuite/internal/tools"
)

// runBuiltinScan handles tools that don't require external binaries.
func (e *Executor) runBuiltinScan(ctx context.Context, scan *database.Scan) {
	e.db.UpdateScanStatus(scan.ID, "running")

	var results []database.Result
	var err error

	switch scan.Tool {
	case "google_dorking":
		results = generateGoogleDorks(scan.ID, scan.Target)
		e.broadcastLines(scan.ID, "Generated Google dork queries for: "+scan.Target)
	case "osint_aggregator":
		results = generateOSINTLinks(scan.ID, scan.Target)
		e.broadcastLines(scan.ID, "Generated OSINT resource links for: "+scan.Target)
	case "ssl_check":
		results, err = checkSSL(scan.ID, scan.Target)
	case "robots_sitemap":
		results, err = fetchRobotsSitemap(ctx, scan.ID, scan.Target)
	case "metadata_extract":
		e.broadcastLines(scan.ID, "Extracting metadata from: "+scan.Target)
		results, err = extractMetadata(ctx, scan.ID, scan.Target)
	}

	if err != nil {
		e.db.UpdateScanStatus(scan.ID, "failed")
		e.broadcaster.Broadcast(scan.ID, tools.OutputLine{
			Timestamp: time.Now(), Stream: "stderr", Line: "Error: " + err.Error(),
		})
	} else {
		if len(results) > 0 {
			for _, r := range results {
				e.broadcaster.Broadcast(scan.ID, tools.OutputLine{
					Timestamp: time.Now(), Stream: "stdout", Line: r.Key + ": " + r.Value,
				})
			}
			e.db.CreateResults(results)
		}
		e.db.UpdateScanStatus(scan.ID, "completed")
	}

	e.broadcaster.Broadcast(scan.ID, tools.OutputLine{Done: true, Timestamp: time.Now()})
}

func (e *Executor) broadcastLines(scanID int64, msg string) {
	for _, line := range strings.Split(msg, "\n") {
		e.broadcaster.Broadcast(scanID, tools.OutputLine{
			Timestamp: time.Now(), Stream: "stdout", Line: line,
		})
	}
}

// --- Google Dorking ---

func generateGoogleDorks(scanID int64, target string) []database.Result {
	dorks := []struct {
		category string
		query    string
	}{
		{"files", fmt.Sprintf(`site:%s filetype:pdf`, target)},
		{"files", fmt.Sprintf(`site:%s filetype:doc OR filetype:docx OR filetype:xls`, target)},
		{"files", fmt.Sprintf(`site:%s filetype:sql OR filetype:bak OR filetype:log`, target)},
		{"login", fmt.Sprintf(`site:%s inurl:login OR inurl:admin OR inurl:signin`, target)},
		{"login", fmt.Sprintf(`site:%s intitle:"index of"`, target)},
		{"sensitive", fmt.Sprintf(`site:%s intext:"password" OR intext:"username" filetype:log`, target)},
		{"sensitive", fmt.Sprintf(`site:%s ext:env OR ext:cfg OR ext:conf`, target)},
		{"subdomains", fmt.Sprintf(`site:*.%s -www`, target)},
		{"technology", fmt.Sprintf(`site:%s inurl:wp-content OR inurl:wp-admin`, target)},
		{"errors", fmt.Sprintf(`site:%s "error" OR "warning" OR "stack trace"`, target)},
	}

	var results []database.Result
	for _, d := range dorks {
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "google_dork",
			Key:        d.category,
			Value:      "https://www.google.com/search?q=" + strings.ReplaceAll(d.query, " ", "+"),
			Details:    fmt.Sprintf(`{"query":"%s"}`, d.query),
		})
	}
	return results
}

// --- OSINT Aggregator ---

func generateOSINTLinks(scanID int64, target string) []database.Result {
	ip := net.ParseIP(target)

	links := []struct {
		name string
		url  string
	}{
		{"VirusTotal", fmt.Sprintf("https://www.virustotal.com/gui/domain/%s", target)},
		{"crt.sh", fmt.Sprintf("https://crt.sh/?q=%%25.%s", target)},
		{"SecurityTrails", fmt.Sprintf("https://securitytrails.com/domain/%s", target)},
		{"DNSDumpster", "https://dnsdumpster.com/"},
		{"Wayback Machine", fmt.Sprintf("https://web.archive.org/web/*/%s", target)},
	}

	if ip != nil {
		links = append(links,
			struct{ name, url string }{"Shodan", fmt.Sprintf("https://www.shodan.io/host/%s", target)},
			struct{ name, url string }{"Censys", fmt.Sprintf("https://search.censys.io/hosts/%s", target)},
			struct{ name, url string }{"GreyNoise", fmt.Sprintf("https://viz.greynoise.io/ip/%s", target)},
			struct{ name, url string }{"AbuseIPDB", fmt.Sprintf("https://www.abuseipdb.com/check/%s", target)},
		)
	} else {
		links = append(links,
			struct{ name, url string }{"Shodan", fmt.Sprintf("https://www.shodan.io/search?query=hostname:%s", target)},
			struct{ name, url string }{"Censys", fmt.Sprintf("https://search.censys.io/search?resource=hosts&q=%s", target)},
		)
	}

	var results []database.Result
	for _, l := range links {
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "osint_link",
			Key:        l.name,
			Value:      l.url,
		})
	}
	return results
}

// --- SSL/TLS Check ---

func checkSSL(scanID int64, target string) ([]database.Result, error) {
	host := target
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", host, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, fmt.Errorf("TLS connection failed: %w", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	var results []database.Result

	results = append(results, database.Result{
		ScanID:     scanID,
		ResultType: "ssl",
		Key:        "tls_version",
		Value:      tlsVersionName(state.Version),
	})

	results = append(results, database.Result{
		ScanID:     scanID,
		ResultType: "ssl",
		Key:        "cipher_suite",
		Value:      tls.CipherSuiteName(state.CipherSuite),
	})

	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "ssl",
			Key:        "subject",
			Value:      cert.Subject.CommonName,
		})
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "ssl",
			Key:        "issuer",
			Value:      cert.Issuer.CommonName,
		})
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "ssl",
			Key:        "not_before",
			Value:      cert.NotBefore.Format(time.RFC3339),
		})
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "ssl",
			Key:        "not_after",
			Value:      cert.NotAfter.Format(time.RFC3339),
		})
		results = append(results, database.Result{
			ScanID:     scanID,
			ResultType: "ssl",
			Key:        "san",
			Value:      strings.Join(cert.DNSNames, ", "),
		})
	}

	return results, nil
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", v)
	}
}

// --- Robots.txt / Sitemap ---

func fetchRobotsSitemap(ctx context.Context, scanID int64, target string) ([]database.Result, error) {
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	target = strings.TrimRight(target, "/")

	client := &http.Client{Timeout: 15 * time.Second}
	var results []database.Result

	// Fetch robots.txt
	robotsURL := target + "/robots.txt"
	req, _ := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			content := string(body)
			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "robots",
				Key:        "robots.txt",
				Value:      content,
			})
			// Parse disallowed paths
			for _, line := range strings.Split(content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(strings.ToLower(line), "disallow:") {
					path := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
					if path != "" {
						results = append(results, database.Result{
							ScanID:     scanID,
							ResultType: "disallowed_path",
							Key:        path,
							Value:      "disallowed",
						})
					}
				}
			}
		}
	}

	// Fetch sitemap.xml
	sitemapURL := target + "/sitemap.xml"
	req2, _ := http.NewRequestWithContext(ctx, "GET", sitemapURL, nil)
	resp2, err := client.Do(req2)
	if err == nil {
		defer resp2.Body.Close()
		if resp2.StatusCode == 200 {
			body, _ := io.ReadAll(io.LimitReader(resp2.Body, 256*1024))
			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "sitemap",
				Key:        "sitemap.xml",
				Value:      string(body),
			})
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("neither robots.txt nor sitemap.xml found")
	}

	return results, nil
}

// --- Metadata Extractor ---

func extractMetadata(ctx context.Context, scanID int64, target string) ([]database.Result, error) {
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "RaccoonRecon/1.0 (Metadata Extractor)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	var results []database.Result

	// HTTP status
	results = append(results, database.Result{
		ScanID: scanID, ResultType: "metadata",
		Key: "http_status", Value: resp.Status,
	})

	// Final URL after redirects
	results = append(results, database.Result{
		ScanID: scanID, ResultType: "metadata",
		Key: "final_url", Value: resp.Request.URL.String(),
	})

	// Interesting response headers
	interestingHeaders := []string{
		"Server", "X-Powered-By", "Content-Type",
		"X-Frame-Options", "X-Content-Type-Options",
		"Strict-Transport-Security", "Content-Security-Policy",
		"X-XSS-Protection", "Access-Control-Allow-Origin",
		"Via", "X-Cache", "X-AspNet-Version", "X-Generator",
	}

	for _, hdr := range interestingHeaders {
		if val := resp.Header.Get(hdr); val != "" {
			results = append(results, database.Result{
				ScanID: scanID, ResultType: "metadata",
				Key: "header:" + strings.ToLower(hdr), Value: val,
			})
		}
	}

	// Read body (limit 2MB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return results, nil
	}

	htmlStr := string(body)

	// Extract <title>
	if title := extractHTMLTag(htmlStr, "title"); title != "" {
		results = append(results, database.Result{
			ScanID: scanID, ResultType: "metadata",
			Key: "title", Value: title,
		})
	}

	// Extract <meta> tags
	metas := parseMetaTags(htmlStr)
	for key, value := range metas {
		results = append(results, database.Result{
			ScanID: scanID, ResultType: "metadata",
			Key: key, Value: value,
		})
	}

	// Extract canonical URL
	if canonical := extractLinkRel(htmlStr, "canonical"); canonical != "" {
		results = append(results, database.Result{
			ScanID: scanID, ResultType: "metadata",
			Key: "canonical", Value: canonical,
		})
	}

	// Extract favicon
	if favicon := extractLinkRel(htmlStr, "icon"); favicon != "" {
		results = append(results, database.Result{
			ScanID: scanID, ResultType: "metadata",
			Key: "favicon", Value: favicon,
		})
	} else if favicon := extractLinkRel(htmlStr, "shortcut icon"); favicon != "" {
		results = append(results, database.Result{
			ScanID: scanID, ResultType: "metadata",
			Key: "favicon", Value: favicon,
		})
	}

	return results, nil
}

// extractHTMLTag extracts text content between <tag> and </tag>.
func extractHTMLTag(html, tag string) string {
	lower := strings.ToLower(html)
	openTag := "<" + tag
	closeTag := "</" + tag + ">"
	start := strings.Index(lower, openTag)
	if start == -1 {
		return ""
	}
	gtPos := strings.Index(lower[start:], ">")
	if gtPos == -1 {
		return ""
	}
	contentStart := start + gtPos + 1
	end := strings.Index(lower[contentStart:], closeTag)
	if end == -1 {
		return ""
	}
	content := strings.TrimSpace(html[contentStart : contentStart+end])
	if len(content) > 500 {
		content = content[:500]
	}
	return content
}

// parseMetaTags extracts <meta name="..." content="..."> and <meta property="..." content="..."> tags.
func parseMetaTags(html string) map[string]string {
	results := make(map[string]string)
	lower := strings.ToLower(html)
	idx := 0

	for {
		pos := strings.Index(lower[idx:], "<meta")
		if pos == -1 {
			break
		}
		pos += idx
		end := strings.Index(lower[pos:], ">")
		if end == -1 {
			break
		}
		tag := html[pos : pos+end+1]
		tagLower := strings.ToLower(tag)

		name := extractAttr(tagLower, "name")
		if name == "" {
			name = extractAttr(tagLower, "property")
		}
		content := extractAttr(tag, "content")

		if name != "" && content != "" {
			name = strings.ToLower(name)
			if len(content) > 500 {
				content = content[:500]
			}
			results[name] = content
		}

		idx = pos + end + 1
	}

	return results
}

// extractAttr extracts the value of an HTML attribute from a tag string.
func extractAttr(tag, attr string) string {
	searchDQ := attr + `="`
	pos := strings.Index(strings.ToLower(tag), searchDQ)
	if pos != -1 {
		start := pos + len(searchDQ)
		end := strings.Index(tag[start:], `"`)
		if end != -1 {
			return tag[start : start+end]
		}
	}

	searchSQ := attr + `='`
	pos = strings.Index(strings.ToLower(tag), searchSQ)
	if pos != -1 {
		start := pos + len(searchSQ)
		end := strings.Index(tag[start:], `'`)
		if end != -1 {
			return tag[start : start+end]
		}
	}

	return ""
}

// extractLinkRel extracts href from <link rel="relValue" href="...">.
func extractLinkRel(html, relValue string) string {
	lower := strings.ToLower(html)
	idx := 0

	for {
		pos := strings.Index(lower[idx:], "<link")
		if pos == -1 {
			break
		}
		pos += idx
		end := strings.Index(lower[pos:], ">")
		if end == -1 {
			break
		}
		tag := html[pos : pos+end+1]
		tagLower := strings.ToLower(tag)

		rel := extractAttr(tagLower, "rel")
		if strings.Contains(strings.ToLower(rel), strings.ToLower(relValue)) {
			href := extractAttr(tag, "href")
			if href != "" {
				return href
			}
		}

		idx = pos + end + 1
	}

	return ""
}
