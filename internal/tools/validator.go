package tools

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var (
	hostnameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	dangerousChars = regexp.MustCompile("[;|&`$(){}\\[\\]!<>\\\\\"']")
)

// ValidateTarget checks that a target is a valid IP, CIDR, or hostname.
func ValidateTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target cannot be empty")
	}

	if dangerousChars.MatchString(target) {
		return fmt.Errorf("target contains invalid characters")
	}

	// Try IP address
	if ip := net.ParseIP(target); ip != nil {
		return nil
	}

	// Try CIDR
	if _, ipNet, err := net.ParseCIDR(target); err == nil {
		ones, bits := ipNet.Mask.Size()
		if bits == 32 && ones < 16 {
			return fmt.Errorf("CIDR range /%d is too large (minimum /16)", ones)
		}
		if bits == 128 && ones < 48 {
			return fmt.Errorf("IPv6 CIDR range /%d is too large (minimum /48)", ones)
		}
		return nil
	}

	// Try hostname
	if !hostnameRegex.MatchString(target) {
		return fmt.Errorf("invalid hostname: %s", target)
	}
	if len(target) > 253 {
		return fmt.Errorf("hostname too long")
	}

	return nil
}

// ValidateURL checks that a target is a valid HTTP/HTTPS URL.
func ValidateURL(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	if dangerousChars.MatchString(target) {
		// Allow : and / for URLs but block other dangerous chars
		cleaned := strings.ReplaceAll(target, ":", "")
		cleaned = strings.ReplaceAll(cleaned, "/", "")
		cleaned = strings.ReplaceAll(cleaned, "?", "")
		cleaned = strings.ReplaceAll(cleaned, "=", "")
		cleaned = strings.ReplaceAll(cleaned, "&", "")
		cleaned = strings.ReplaceAll(cleaned, ".", "")
		cleaned = strings.ReplaceAll(cleaned, "-", "")
		cleaned = strings.ReplaceAll(cleaned, "_", "")
		cleaned = strings.ReplaceAll(cleaned, "%", "")
		if dangerousChars.MatchString(cleaned) {
			return fmt.Errorf("URL contains invalid characters")
		}
	}

	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}

	return nil
}

// SanitizeArg strips any shell metacharacters from a single argument.
func SanitizeArg(arg string) string {
	return dangerousChars.ReplaceAllString(arg, "")
}
