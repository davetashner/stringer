package collectors

import (
	"encoding/xml"
)

// csprojProject represents the top-level structure of a .csproj file.
type csprojProject struct {
	XMLName    xml.Name          `xml:"Project"`
	ItemGroups []csprojItemGroup `xml:"ItemGroup"`
}

// csprojItemGroup represents an <ItemGroup> element containing package references.
type csprojItemGroup struct {
	PackageRefs []csprojPackageRef `xml:"PackageReference"`
}

// csprojPackageRef represents a <PackageReference> element in a .csproj file.
// Version can be specified as an attribute or a child element.
type csprojPackageRef struct {
	Include     string `xml:"Include,attr"`
	Version     string `xml:"Version,attr"`
	VersionElem string `xml:"Version"`
}

// parseCsprojDeps parses a .csproj file and returns PackageQuery entries for OSV lookup.
// It handles both version styles:
//   - Attribute: <PackageReference Include="Foo" Version="1.0" />
//   - Child element: <PackageReference Include="Foo"><Version>1.0</Version></PackageReference>
//
// Entries with empty Include or Version are skipped. Duplicates are deduplicated by package name.
func parseCsprojDeps(data []byte) ([]PackageQuery, error) {
	var project csprojProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, ig := range project.ItemGroups {
		for _, ref := range ig.PackageRefs {
			if ref.Include == "" {
				continue
			}

			version := ref.Version
			if version == "" {
				version = ref.VersionElem
			}
			if version == "" {
				continue
			}

			if seen[ref.Include] {
				continue
			}
			seen[ref.Include] = true

			queries = append(queries, PackageQuery{
				Ecosystem: "NuGet",
				Name:      ref.Include,
				Version:   version,
			})
		}
	}

	return queries, nil
}
