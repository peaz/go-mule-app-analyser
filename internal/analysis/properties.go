package analysis

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tokenPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// loadProperties reads all *.properties files found directly under
// src/main/resources/ in the given app directory and returns a merged
// key=value map.
//
// Missing properties files and absent resource directories are treated as
// graceful no-ops — customers commonly omit or redact sensitive values.
// Any parse warnings are appended to the supplied warnings slice.
func loadProperties(appDir string) (map[string]string, []string) {
	props := make(map[string]string)
	var warnings []string

	resourceDir := filepath.Join(appDir, "src", "main", "resources")
	entries, err := os.ReadDir(resourceDir)
	if err != nil {
		// No resources directory — expected for some apps
		return props, warnings
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".properties") {
			continue
		}
		path := filepath.Join(resourceDir, entry.Name())
		fileProps, warn := parsePropertiesFile(path)
		if warn != "" {
			warnings = append(warnings, warn)
		}
		// first file wins for duplicate keys (environment-agnostic union)
		for k, v := range fileProps {
			if _, exists := props[k]; !exists {
				props[k] = v
			}
		}
	}

	return props, warnings
}

func parsePropertiesFile(path string) (map[string]string, string) {
	props := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return props, "could not read properties file: " + path
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			idx = strings.IndexByte(line, ':')
		}
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key != "" {
			props[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return props, fmt.Sprintf("error reading properties file %s: %v", path, err)
	}
	return props, ""
}

// resolveToken substitutes all ${token} placeholders using the property map.
// Returns (resolved string, true) when every token was resolved, or
// (partially resolved string, false) when at least one token remains.
func resolveToken(value string, props map[string]string) (string, bool) {
	if !strings.Contains(value, "${") {
		return value, true
	}
	allResolved := true
	result := tokenPattern.ReplaceAllStringFunc(value, func(match string) string {
		key := match[2 : len(match)-1]
		if v, ok := props[key]; ok {
			return v
		}
		allResolved = false
		return match
	})
	return result, allResolved
}

// unresolvedTokensIn returns the unique ${token} keys in value that are absent
// from props.
func unresolvedTokensIn(value string, props map[string]string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, match := range tokenPattern.FindAllString(value, -1) {
		key := match[2 : len(match)-1]
		if _, ok := props[key]; !ok && !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}
	return result
}
