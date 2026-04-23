package analysis

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// RAMLInfo holds identity fields extracted from a RAML API specification file.
type RAMLInfo struct {
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
	BaseURI string `json:"baseUri,omitempty"`
}

// extractRAML scans src/main/resources/api/ for a root RAML file and returns
// its identity fields. Returns nil when no valid RAML file is found.
func extractRAML(appDir string) *RAMLInfo {
	ramlDir := filepath.Join(appDir, "src", "main", "resources", "api")
	entries, err := os.ReadDir(ramlDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".raml") {
			continue
		}
		path := filepath.Join(ramlDir, entry.Name())
		if info := parseRAMLHeader(path); info != nil {
			return info
		}
	}
	return nil
}

// parseRAMLHeader reads only the header section of a RAML file (first 30 lines)
// and extracts title, version, and baseUri fields.
func parseRAMLHeader(path string) *RAMLInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info := &RAMLInfo{}
	scanner := bufio.NewScanner(f)
	lineCount := 0
	isRAML := false

	for scanner.Scan() && lineCount < 30 {
		line := strings.TrimSpace(scanner.Text())
		lineCount++

		if lineCount == 1 {
			if !strings.HasPrefix(line, "#%RAML") {
				return nil
			}
			isRAML = true
			continue
		}

		if !isRAML {
			return nil
		}

		// Stop scanning when we hit a block that's clearly not header fields
		if strings.HasPrefix(line, "types:") || strings.HasPrefix(line, "traits:") ||
			strings.HasPrefix(line, "resourceTypes:") || strings.HasPrefix(line, "/") {
			break
		}

		key, val, ok := splitYAMLLine(line)
		if !ok {
			continue
		}
		switch key {
		case "title":
			info.Title = val
		case "version":
			info.Version = val
		case "baseUri":
			info.BaseURI = val
		}
	}

	if info.Title == "" {
		return nil
	}
	return info
}

// splitYAMLLine splits a "key: value" YAML line.  Returns ok=false for
// comment lines, blank lines, or lines without a colon.
func splitYAMLLine(line string) (key, value string, ok bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	// strip surrounding quotes
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}
