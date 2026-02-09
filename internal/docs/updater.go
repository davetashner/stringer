package docs

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// markerStart is the prefix for auto-section start markers.
const markerStart = "<!-- stringer:auto:start:"

// markerEnd is the prefix for auto-section end markers.
const markerEnd = "<!-- stringer:auto:end:"

// Update regenerates auto-sections in an existing AGENTS.md while preserving
// manual content. It reads the existing file, regenerates auto-sections from
// the analysis, and writes the result to w.
func Update(existingPath string, analysis *RepoAnalysis, w io.Writer) error {
	// Generate fresh content.
	var freshBuf strings.Builder
	if err := Generate(analysis, &freshBuf); err != nil {
		return fmt.Errorf("generate fresh content: %w", err)
	}

	// Parse auto-sections from fresh content.
	freshSections := parseAutoSections(freshBuf.String())

	// Read existing file.
	existingContent, err := FS.ReadFile(existingPath)
	if err != nil {
		return fmt.Errorf("read existing file: %w", err)
	}

	// Replace auto-sections in existing content.
	result := replaceAutoSections(string(existingContent), freshSections)

	_, err = io.WriteString(w, result)
	return err
}

// parseAutoSections extracts auto-generated sections from content.
// Returns a map of section name -> content (including markers).
func parseAutoSections(content string) map[string]string {
	sections := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentSection string
	var sectionContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), markerStart) {
			name := strings.TrimPrefix(strings.TrimSpace(line), markerStart)
			name = strings.TrimSuffix(name, " -->")
			currentSection = name
			sectionContent.Reset()
			sectionContent.WriteString(line + "\n")
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), markerEnd) && currentSection != "" {
			sectionContent.WriteString(line + "\n")
			sections[currentSection] = sectionContent.String()
			currentSection = ""
			sectionContent.Reset()
			continue
		}

		if currentSection != "" {
			sectionContent.WriteString(line + "\n")
		}
	}

	return sections
}

// replaceAutoSections replaces auto-generated sections in existing content
// with fresh versions, preserving everything else.
func replaceAutoSections(existing string, freshSections map[string]string) string {
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(existing))

	var skipSection bool

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), markerStart) {
			name := strings.TrimPrefix(strings.TrimSpace(line), markerStart)
			name = strings.TrimSuffix(name, " -->")
			if fresh, ok := freshSections[name]; ok {
				result.WriteString(fresh)
				skipSection = true
				continue
			}
		}

		if strings.HasPrefix(strings.TrimSpace(line), markerEnd) && skipSection {
			skipSection = false
			continue
		}

		if !skipSection {
			result.WriteString(line + "\n")
		}
	}

	return result.String()
}
