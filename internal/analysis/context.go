package analysis

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadCustomContext reads the custom context file and returns its contents.
// Returns an empty string (not an error) if the file does not exist.
func LoadCustomContext(contextFile string) (string, error) {
	data, err := os.ReadFile(contextFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading custom context file: %w", err)
	}
	return string(data), nil
}

// SaveCustomContext writes content to the custom context file.
// Creates parent directories if they do not exist.
func SaveCustomContext(contextFile, content string) error {
	dir := filepath.Dir(contextFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating context directory: %w", err)
	}
	if err := os.WriteFile(contextFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing custom context file: %w", err)
	}
	return nil
}

// ClearCustomContext removes the custom context file.
// Returns nil if the file does not exist.
func ClearCustomContext(contextFile string) error {
	err := os.Remove(contextFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing custom context file: %w", err)
	}
	return nil
}
