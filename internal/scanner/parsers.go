package scanner

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/jamesruggles/reconsuite/internal/database"
)

// --- WHOIS Parser ---

func parseWhoisResults(scanID int64, raw string) []database.Result {
	var results []database.Result
	lines := strings.Split(raw, "\n")

	interestingFields := map[string]string{
		"Registrar:":                 "registrar",
		"Registrant Organization:":   "registrant_org",
		"Creation Date:":             "creation_date",
		"Updated Date:":              "updated_date",
		"Registry Expiry Date:":      "expiry_date",
		"Name Server:":               "nameserver",
		"Registrant Country:":        "registrant_country",
		"Registrant State/Province:": "registrant_state",
		"DNSSEC:":                    "dnssec",
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		for prefix, rtype := range interestingFields {
			if strings.HasPrefix(line, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if value != "" {
					results = append(results, database.Result{
						ScanID:     scanID,
						ResultType: "whois",
						Key:        rtype,
						Value:      value,
					})
				}
			}
		}
	}

	return results
}

// --- DNS/Dig Parser ---

func parseDigResults(scanID int64, raw string) []database.Result {
	var results []database.Result
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 5 {
			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "dns",
				Key:        fields[3], // record type (A, MX, NS, etc.)
				Value:      strings.Join(fields[4:], " "),
				Details:    fmt.Sprintf(`{"name":"%s","ttl":"%s","class":"%s"}`, fields[0], fields[1], fields[2]),
			})
		}
	}

	return results
}

// --- Nmap XML Parser ---

type nmapRun struct {
	XMLName xml.Name   `xml:"nmaprun"`
	Hosts   []nmapHost `xml:"host"`
}

type nmapHost struct {
	Addresses []nmapAddress `xml:"address"`
	Hostnames []nmapHostname `xml:"hostnames>hostname"`
	Ports     nmapPorts     `xml:"ports"`
	OS        nmapOS        `xml:"os"`
}

type nmapAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type nmapHostname struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type nmapPorts struct {
	Ports []nmapPort `xml:"port"`
}

type nmapPort struct {
	Protocol string      `xml:"protocol,attr"`
	PortID   string      `xml:"portid,attr"`
	State    nmapState   `xml:"state"`
	Service  nmapService `xml:"service"`
}

type nmapState struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

type nmapService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

type nmapOS struct {
	OSMatches []nmapOSMatch `xml:"osmatch"`
}

type nmapOSMatch struct {
	Name     string `xml:"name,attr"`
	Accuracy string `xml:"accuracy,attr"`
}

func parseNmapResults(scanID int64, raw string) []database.Result {
	var results []database.Result

	var run nmapRun
	if err := xml.Unmarshal([]byte(raw), &run); err != nil {
		return nil
	}

	for _, host := range run.Hosts {
		addr := ""
		for _, a := range host.Addresses {
			if a.AddrType == "ipv4" || a.AddrType == "ipv6" {
				addr = a.Addr
				break
			}
		}

		for _, port := range host.Ports.Ports {
			svcInfo := port.Service.Name
			if port.Service.Product != "" {
				svcInfo += " (" + port.Service.Product
				if port.Service.Version != "" {
					svcInfo += " " + port.Service.Version
				}
				svcInfo += ")"
			}

			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "port",
				Key:        port.PortID + "/" + port.Protocol,
				Value:      port.State.State,
				Details:    fmt.Sprintf(`{"host":"%s","service":"%s","reason":"%s"}`, addr, svcInfo, port.State.Reason),
			})
		}

		for _, osMatch := range host.OS.OSMatches {
			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "os",
				Key:        "os_match",
				Value:      osMatch.Name,
				Details:    fmt.Sprintf(`{"accuracy":"%s","host":"%s"}`, osMatch.Accuracy, addr),
			})
		}
	}

	return results
}

// --- Curl/HTTP Header Parser ---

func parseCurlResults(scanID int64, raw string) []database.Result {
	var results []database.Result
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "HTTP/") {
			if strings.HasPrefix(line, "HTTP/") {
				parts := strings.SplitN(line, " ", 3)
				if len(parts) >= 2 {
					results = append(results, database.Result{
						ScanID:     scanID,
						ResultType: "header",
						Key:        "status",
						Value:      strings.Join(parts[1:], " "),
					})
				}
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			results = append(results, database.Result{
				ScanID:     scanID,
				ResultType: "header",
				Key:        strings.TrimSpace(parts[0]),
				Value:      strings.TrimSpace(parts[1]),
			})
		}
	}

	return results
}
