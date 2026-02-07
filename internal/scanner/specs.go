package scanner

import (
	"fmt"
	"strings"
	"time"

	"github.com/jamesruggles/reconsuite/internal/tools"
)

func buildWhoisSpec(target string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	return tools.ToolSpec{
		Name:       "WHOIS Lookup",
		BinaryName: "whois",
		Args:       []string{target},
		Timeout:    30 * time.Second,
	}, nil
}

func buildDigSpec(target, recordType string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	if recordType == "" {
		recordType = "ANY"
	}
	validTypes := map[string]bool{"A": true, "AAAA": true, "MX": true, "NS": true, "TXT": true, "SOA": true, "CNAME": true, "PTR": true, "ANY": true}
	if !validTypes[strings.ToUpper(recordType)] {
		return tools.ToolSpec{}, fmt.Errorf("invalid record type: %s", recordType)
	}
	return tools.ToolSpec{
		Name:       "DNS Lookup (" + strings.ToUpper(recordType) + ")",
		BinaryName: "dig",
		Args:       []string{target, strings.ToUpper(recordType), "+noall", "+answer", "+authority"},
		Timeout:    30 * time.Second,
	}, nil
}

func buildTheHarvesterSpec(domain, sources string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(domain); err != nil {
		return tools.ToolSpec{}, err
	}
	if sources == "" {
		sources = "bing,crtsh,dnsdumpster"
	}
	return tools.ToolSpec{
		Name:       "theHarvester",
		BinaryName: "theHarvester",
		Args:       []string{"-d", domain, "-b", sources},
		Timeout:    5 * time.Minute,
	}, nil
}

func buildDnsReconSpec(target, scanMode string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	args := []string{"-d", target}
	switch scanMode {
	case "reverse":
		args = []string{"-r", target}
	case "axfr":
		args = []string{"-d", target, "-t", "axfr"}
	}
	return tools.ToolSpec{
		Name:       "DNSRecon",
		BinaryName: "dnsrecon",
		Args:       args,
		Timeout:    5 * time.Minute,
	}, nil
}

func buildNmapSpec(target string, params map[string]string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}

	args := []string{"-T4"}
	scanType := params["scan_type"]

	switch scanType {
	case "service":
		args = append(args, "-sV")
	case "os":
		args = append(args, "-O")
	case "ping":
		args = append(args, "-sn")
	case "banner":
		args = append(args, "--script=banner")
	default:
		// Default port scan
		args = append(args, "-sT")
	}

	if ports := params["ports"]; ports != "" {
		args = append(args, "-p", tools.SanitizeArg(ports))
	}

	// Use XML output for parsing
	args = append(args, "-oX", "-", target)

	return tools.ToolSpec{
		Name:       "Nmap",
		BinaryName: "nmap",
		Args:       args,
		Timeout:    30 * time.Minute,
	}, nil
}

func buildTracerouteSpec(target string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	return tools.ToolSpec{
		Name:       "Traceroute",
		BinaryName: "traceroute",
		Args:       []string{target},
		Timeout:    2 * time.Minute,
	}, nil
}

func buildSnmpWalkSpec(target, community, oid string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	if community == "" {
		community = "public"
	}
	if oid == "" {
		oid = "1.3.6.1.2.1"
	}
	return tools.ToolSpec{
		Name:       "SNMP Walk",
		BinaryName: "snmpwalk",
		Args:       []string{"-v2c", "-c", community, target, oid},
		Timeout:    2 * time.Minute,
	}, nil
}

func buildNetcatSpec(target, port string) (tools.ToolSpec, error) {
	if err := tools.ValidateTarget(target); err != nil {
		return tools.ToolSpec{}, err
	}
	if port == "" {
		return tools.ToolSpec{}, fmt.Errorf("port is required for banner grab")
	}
	return tools.ToolSpec{
		Name:       "Banner Grab",
		BinaryName: "nc",
		Args:       []string{"-w", "5", "-v", target, tools.SanitizeArg(port)},
		Timeout:    30 * time.Second,
	}, nil
}

func buildCurlSpec(target string) (tools.ToolSpec, error) {
	if err := tools.ValidateURL(target); err != nil {
		return tools.ToolSpec{}, err
	}
	return tools.ToolSpec{
		Name:       "HTTP Headers",
		BinaryName: "curl",
		Args:       []string{"-I", "-s", "-L", "--max-time", "15", target},
		Timeout:    30 * time.Second,
	}, nil
}

func buildWhatWebSpec(target, aggression string) (tools.ToolSpec, error) {
	if err := tools.ValidateURL(target); err != nil {
		return tools.ToolSpec{}, err
	}
	if aggression == "" {
		aggression = "1"
	}
	return tools.ToolSpec{
		Name:       "WhatWeb",
		BinaryName: "whatweb",
		Args:       []string{"-a", aggression, "--color=never", target},
		Timeout:    2 * time.Minute,
	}, nil
}

func buildGobusterSpec(target, wordlist, extensions string) (tools.ToolSpec, error) {
	if err := tools.ValidateURL(target); err != nil {
		return tools.ToolSpec{}, err
	}
	if wordlist == "" {
		wordlist = "/usr/share/wordlists/dirb/common.txt"
	}
	args := []string{"dir", "-u", target, "-w", wordlist, "-t", "10", "--no-color", "-q"}
	if extensions != "" {
		args = append(args, "-x", tools.SanitizeArg(extensions))
	}
	return tools.ToolSpec{
		Name:       "Gobuster",
		BinaryName: "gobuster",
		Args:       args,
		Timeout:    15 * time.Minute,
	}, nil
}
