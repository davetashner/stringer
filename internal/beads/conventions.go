package beads

import "strings"

// Conventions holds detected beads conventions from existing issues.
type Conventions struct {
	// IDPrefix is the dominant ID prefix (e.g., "stringer-", "str-", "app-").
	IDPrefix string

	// LabelStyle is the detected label style: "kebab-case" or "snake_case".
	LabelStyle string

	// UseIssueType indicates whether existing beads use "issue_type" vs "type".
	UseIssueType bool

	// MinPriority is the minimum priority value seen.
	MinPriority int

	// MaxPriority is the maximum priority value seen.
	MaxPriority int
}

// DetectConventions analyzes existing beads to detect naming conventions.
func DetectConventions(existing []Bead) *Conventions {
	if len(existing) == 0 {
		return nil
	}

	c := &Conventions{
		MinPriority: 999,
		MaxPriority: -1,
	}

	// Count ID prefixes.
	prefixCounts := make(map[string]int)
	for _, b := range existing {
		prefix := extractPrefix(b.ID)
		if prefix != "" {
			prefixCounts[prefix]++
		}

		// Track priority range.
		if b.Priority < c.MinPriority {
			c.MinPriority = b.Priority
		}
		if b.Priority > c.MaxPriority {
			c.MaxPriority = b.Priority
		}

		// Detect type field usage.
		if b.IssueType != "" {
			c.UseIssueType = true
		}
	}

	// Pick dominant prefix.
	maxCount := 0
	for prefix, count := range prefixCounts {
		if count > maxCount {
			maxCount = count
			c.IDPrefix = prefix
		}
	}

	// Detect label style from existing labels.
	c.LabelStyle = detectLabelStyle(existing)

	// Normalize priority range.
	if c.MinPriority == 999 {
		c.MinPriority = 0
	}
	if c.MaxPriority == -1 {
		c.MaxPriority = 4
	}

	return c
}

// extractPrefix extracts the prefix from a bead ID (everything up to and including
// the last hyphen before the alphanumeric suffix).
// e.g., "stringer-abc" -> "stringer-", "str-0e4098f9" -> "str-", "app-v2-xyz" -> "app-v2-"
func extractPrefix(id string) string {
	lastHyphen := strings.LastIndex(id, "-")
	if lastHyphen < 0 {
		return ""
	}
	return id[:lastHyphen+1]
}

// detectLabelStyle checks whether existing beads use kebab-case or snake_case labels.
func detectLabelStyle(existing []Bead) string {
	kebab := 0
	snake := 0

	for _, b := range existing {
		for _, label := range b.Labels {
			if strings.Contains(label, "-") {
				kebab++
			}
			if strings.Contains(label, "_") {
				snake++
			}
		}
	}

	if snake > kebab {
		return "snake_case"
	}
	return "kebab-case"
}
