package collectors

import (
	"encoding/xml"
	"log/slog"
	"strings"
)

// pomProject represents the top-level structure of a Maven pom.xml file.
type pomProject struct {
	XMLName              xml.Name        `xml:"project"`
	GroupID              string          `xml:"groupId"`
	ArtifactID           string          `xml:"artifactId"`
	Version              string          `xml:"version"`
	Properties           pomProperties   `xml:"properties"`
	Dependencies         []pomDependency `xml:"dependencies>dependency"`
	DependencyManagement struct {
		Dependencies []pomDependency `xml:"dependencies>dependency"`
	} `xml:"dependencyManagement"`
}

// pomProperties captures arbitrary <properties> entries as raw XML tokens.
type pomProperties struct {
	Entries []pomProperty `xml:",any"`
}

// pomProperty represents a single property element (e.g., <spring.version>5.3.0</spring.version>).
type pomProperty struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// pomDependency represents a single <dependency> element in pom.xml.
type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

// parseMavenDeps reads a pom.xml file and returns PackageQuery entries for OSV lookup.
// It extracts dependencies from both <dependencies> and <dependencyManagement> sections,
// performs property interpolation for ${...} references, and skips test-scoped
// dependencies and those with unresolvable versions.
func parseMavenDeps(data []byte) ([]PackageQuery, error) {
	var project pomProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, err
	}

	// Build property map from <properties> block.
	props := buildPropertyMap(&project)

	// Collect dependencies from both sections, deduplicating by groupId:artifactId.
	seen := make(map[string]bool)
	var queries []PackageQuery

	allDeps := make([]pomDependency, 0, len(project.Dependencies)+len(project.DependencyManagement.Dependencies))
	allDeps = append(allDeps, project.Dependencies...)
	allDeps = append(allDeps, project.DependencyManagement.Dependencies...)

	for _, dep := range allDeps {
		groupID := strings.TrimSpace(dep.GroupID)
		artifactID := strings.TrimSpace(dep.ArtifactID)
		version := strings.TrimSpace(dep.Version)
		scope := strings.TrimSpace(dep.Scope)

		if groupID == "" || artifactID == "" {
			continue
		}

		// Skip test-scoped dependencies.
		if strings.EqualFold(scope, "test") {
			continue
		}

		// Resolve property references in version.
		version = resolveProperties(version, props)

		// Skip if version is empty or still contains unresolved property references.
		if version == "" || strings.Contains(version, "${") {
			if dep.Version != "" && strings.Contains(dep.Version, "${") {
				slog.Warn("maven: skipping dependency with unresolvable version property",
					"groupId", groupID,
					"artifactId", artifactID,
					"version", dep.Version,
				)
			}
			continue
		}

		// Maven OSV ecosystem uses groupId:artifactId as the package name.
		name := groupID + ":" + artifactID
		if seen[name] {
			continue
		}
		seen[name] = true

		queries = append(queries, PackageQuery{
			Ecosystem: "Maven",
			Name:      name,
			Version:   version,
		})
	}

	return queries, nil
}

// buildPropertyMap extracts properties from the pom.xml <properties> block
// and adds built-in properties like project.version, project.groupId, and
// project.artifactId.
func buildPropertyMap(project *pomProject) map[string]string {
	props := make(map[string]string, len(project.Properties.Entries)+3)

	// Built-in Maven properties.
	if project.Version != "" {
		props["project.version"] = project.Version
	}
	if project.GroupID != "" {
		props["project.groupId"] = project.GroupID
	}
	if project.ArtifactID != "" {
		props["project.artifactId"] = project.ArtifactID
	}

	// User-defined properties from <properties> block.
	for _, entry := range project.Properties.Entries {
		key := entry.XMLName.Local
		value := strings.TrimSpace(entry.Value)
		if key != "" && value != "" {
			props[key] = value
		}
	}

	return props
}

// resolveProperties replaces ${property.name} references in s with values
// from the props map. It performs a single pass — nested property references
// are not supported.
func resolveProperties(s string, props map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}

	result := s
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		key := result[start+2 : end]
		val, ok := props[key]
		if !ok {
			// Leave unresolved — caller will detect remaining ${...} and skip.
			break
		}
		result = result[:start] + val + result[end+1:]
	}

	return result
}
