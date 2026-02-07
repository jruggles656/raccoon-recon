package tools

import (
	"os/exec"
	"strings"
)

type ToolStatus struct {
	Name      string `json:"name"`
	Binary    string `json:"binary"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
}

var requiredTools = []struct {
	name       string
	binary     string
	versionArg string
}{
	{"Nmap", "nmap", "--version"},
	{"theHarvester", "theHarvester", "--help"},
	{"DNSRecon", "dnsrecon", "--help"},
	{"WhatWeb", "whatweb", "--version"},
	{"WHOIS", "whois", ""},
	{"dig", "dig", "-v"},
	{"curl", "curl", "--version"},
	{"Gobuster", "gobuster", "version"},
	{"Traceroute", "traceroute", "--version"},
	{"Netcat", "nc", "-h"},
}

func DetectAll() []ToolStatus {
	var statuses []ToolStatus

	for _, tool := range requiredTools {
		status := ToolStatus{
			Name:   tool.name,
			Binary: tool.binary,
		}

		path, err := exec.LookPath(tool.binary)
		if err != nil {
			status.Installed = false
		} else {
			status.Installed = true
			status.Path = path

			if tool.versionArg != "" {
				out, err := exec.Command(tool.binary, tool.versionArg).CombinedOutput()
				if err == nil {
					version := strings.TrimSpace(string(out))
					if len(version) > 100 {
						version = version[:100]
					}
					// Extract first line
					if idx := strings.IndexByte(version, '\n'); idx > 0 {
						version = version[:idx]
					}
					status.Version = version
				}
			}
		}

		statuses = append(statuses, status)
	}

	return statuses
}
