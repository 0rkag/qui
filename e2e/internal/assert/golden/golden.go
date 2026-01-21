// Package golden provides golden file testing utilities for API responses.
package golden

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

// Assert compares actual data against a golden file.
// If -update-golden flag is set, it updates the golden file instead.
func Assert(t *testing.T, name string, actual []byte) {
	t.Helper()

	goldenPath := filepath.Join("../golden", name+".golden.json")

	if *updateGolden {
		// Normalize before saving
		normalized := normalizeJSON(t, actual)
		formatted := formatJSON(t, normalized)

		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create golden dir: %v", err)
		}

		if err := os.WriteFile(goldenPath, formatted, 0o644); err != nil {
			t.Fatalf("failed to write golden file: %v", err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	// Read expected golden file
	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("golden file not found: %s\nRun with -update-golden to create it", goldenPath)
	}
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	// Normalize actual response
	normalizedActual := normalizeJSON(t, actual)
	formattedActual := formatJSON(t, normalizedActual)

	// Compare
	if string(formattedActual) != string(expected) {
		t.Errorf("response mismatch for %s\n\nExpected:\n%s\n\nActual:\n%s",
			name, string(expected), string(formattedActual))
	}
}

// AssertStructure compares only the structure (keys and types) of JSON responses.
// Values are normalized to type placeholders.
func AssertStructure(t *testing.T, name string, actual []byte) {
	t.Helper()

	goldenPath := filepath.Join("../golden", name+".structure.json")

	// Extract structure only
	structure := extractStructure(t, actual)
	formatted := formatJSON(t, structure)

	if *updateGolden {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create golden dir: %v", err)
		}

		if err := os.WriteFile(goldenPath, formatted, 0o644); err != nil {
			t.Fatalf("failed to write golden file: %v", err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("golden file not found: %s\nRun with -update-golden to create it", goldenPath)
	}
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	if string(formatted) != string(expected) {
		t.Errorf("structure mismatch for %s\n\nExpected:\n%s\n\nActual:\n%s",
			name, string(expected), string(formatted))
	}
}

// normalizeJSON replaces dynamic values with placeholders.
func normalizeJSON(t *testing.T, data []byte) []byte {
	t.Helper()

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	normalized := normalizeValue(v)

	result, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("failed to marshal normalized JSON: %v", err)
	}
	return result
}

// normalizeValue recursively normalizes JSON values.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return normalizeObject(val)
	case []any:
		return normalizeArray(val)
	default:
		return v
	}
}

// normalizeObject normalizes an object, replacing dynamic field values.
func normalizeObject(obj map[string]any) map[string]any {
	result := make(map[string]any)
	for key, val := range obj {
		result[key] = normalizeField(key, val)
	}
	return result
}

// normalizeField normalizes a field based on its key name.
func normalizeField(key string, val any) any {
	keyLower := strings.ToLower(key)

	// Dynamic fields that should be normalized
	switch {
	case keyLower == "id":
		if _, ok := val.(float64); ok {
			return "<ID>"
		}
	case keyLower == "host" || keyLower == "url":
		if s, ok := val.(string); ok && (strings.Contains(s, "localhost") || strings.Contains(s, "127.0.0.1")) {
			return "<HOST>"
		}
	case keyLower == "password":
		return "<PASSWORD>"
	case strings.HasSuffix(keyLower, "_on") || strings.HasSuffix(keyLower, "on"):
		// Timestamps like added_on, completed_on
		if _, ok := val.(float64); ok {
			return "<TIMESTAMP>"
		}
	case keyLower == "hash":
		if s, ok := val.(string); ok && len(s) == 40 {
			return val // Keep hash, it's deterministic from magnet
		}
	case keyLower == "save_path" || keyLower == "savepath":
		if s, ok := val.(string); ok && strings.HasPrefix(s, "/") {
			return "<PATH>"
		}
	case keyLower == "webapiver" || keyLower == "webapiversion":
		return "<VERSION>"
	}

	// Recurse into nested structures
	return normalizeValue(val)
}

// normalizeArray normalizes an array.
func normalizeArray(arr []any) []any {
	result := make([]any, len(arr))
	for i, val := range arr {
		result[i] = normalizeValue(val)
	}
	return result
}

// extractStructure extracts only the structure of JSON, replacing values with type names.
func extractStructure(t *testing.T, data []byte) []byte {
	t.Helper()

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	structure := extractValueStructure(v)

	result, err := json.Marshal(structure)
	if err != nil {
		t.Fatalf("failed to marshal structure: %v", err)
	}
	return result
}

// extractValueStructure recursively extracts structure.
func extractValueStructure(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, value := range val {
			result[key] = extractValueStructure(value)
		}
		return result
	case []any:
		if len(val) == 0 {
			return []any{}
		}
		// For arrays, just show structure of first element
		return []any{extractValueStructure(val[0])}
	case string:
		return "<string>"
	case float64:
		// Check if it's actually an integer
		if val == float64(int64(val)) {
			return "<int>"
		}
		return "<float>"
	case bool:
		return "<bool>"
	case nil:
		return "<null>"
	default:
		return "<unknown>"
	}
}

// formatJSON formats JSON with indentation.
func formatJSON(t *testing.T, data []byte) []byte {
	t.Helper()

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed to parse JSON for formatting: %v", err)
	}

	formatted, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("failed to format JSON: %v", err)
	}
	return append(formatted, '\n')
}

// DynamicFieldPatterns contains regex patterns for identifying dynamic content.
var DynamicFieldPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"id":\s*\d+`),
	regexp.MustCompile(`"host":\s*"[^"]*localhost[^"]*"`),
	regexp.MustCompile(`"added_on":\s*\d+`),
	regexp.MustCompile(`"save_path":\s*"/[^"]*"`),
}
