// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMavenDeps_SingleDependency(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.apache.commons</groupId>
      <artifactId>commons-lang3</artifactId>
      <version>3.12.0</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "Maven", queries[0].Ecosystem)
	assert.Equal(t, "org.apache.commons:commons-lang3", queries[0].Name)
	assert.Equal(t, "3.12.0", queries[0].Version)
}

func TestParseMavenDeps_MultipleDependencies(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.apache.commons</groupId>
      <artifactId>commons-lang3</artifactId>
      <version>3.12.0</version>
    </dependency>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.1-jre</version>
    </dependency>
    <dependency>
      <groupId>org.slf4j</groupId>
      <artifactId>slf4j-api</artifactId>
      <version>2.0.9</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 3)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
		assert.Equal(t, "Maven", q.Ecosystem)
	}

	assert.Equal(t, "3.12.0", names["org.apache.commons:commons-lang3"])
	assert.Equal(t, "31.1-jre", names["com.google.guava:guava"])
	assert.Equal(t, "2.0.9", names["org.slf4j:slf4j-api"])
}

func TestParseMavenDeps_PropertyInterpolation_ProjectVersion(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>2.5.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>shared-lib</artifactId>
      <version>${project.version}</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "com.example:shared-lib", queries[0].Name)
	assert.Equal(t, "2.5.0", queries[0].Version)
}

func TestParseMavenDeps_PropertyInterpolation_CustomProperties(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <properties>
    <spring.version>6.1.3</spring.version>
    <jackson.version>2.16.1</jackson.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>${spring.version}</version>
    </dependency>
    <dependency>
      <groupId>com.fasterxml.jackson.core</groupId>
      <artifactId>jackson-databind</artifactId>
      <version>${jackson.version}</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	byName := make(map[string]string)
	for _, q := range queries {
		byName[q.Name] = q.Version
	}

	assert.Equal(t, "6.1.3", byName["org.springframework:spring-core"])
	assert.Equal(t, "2.16.1", byName["com.fasterxml.jackson.core:jackson-databind"])
}

func TestParseMavenDeps_DependencyManagement(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.apache.logging.log4j</groupId>
        <artifactId>log4j-core</artifactId>
        <version>2.22.1</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "Maven", queries[0].Ecosystem)
	assert.Equal(t, "org.apache.logging.log4j:log4j-core", queries[0].Name)
	assert.Equal(t, "2.22.1", queries[0].Version)
}

func TestParseMavenDeps_BothSections(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.apache.logging.log4j</groupId>
        <artifactId>log4j-core</artifactId>
        <version>2.22.1</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
  <dependencies>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.1-jre</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}

	assert.Equal(t, "31.1-jre", names["com.google.guava:guava"])
	assert.Equal(t, "2.22.1", names["org.apache.logging.log4j:log4j-core"])
}

func TestParseMavenDeps_TestScopeSkipped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.1-jre</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
      <scope>test</scope>
    </dependency>
    <dependency>
      <groupId>org.mockito</groupId>
      <artifactId>mockito-core</artifactId>
      <version>5.8.0</version>
      <scope>Test</scope>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "com.google.guava:guava", queries[0].Name)
}

func TestParseMavenDeps_NoVersion_Skipped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.1-jre</version>
    </dependency>
    <dependency>
      <groupId>org.slf4j</groupId>
      <artifactId>slf4j-api</artifactId>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "com.google.guava:guava", queries[0].Name)
}

func TestParseMavenDeps_UnresolvableProperty_Skipped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <properties>
    <spring.version>6.1.3</spring.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>${spring.version}</version>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>unknown-lib</artifactId>
      <version>${undefined.property}</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "org.springframework:spring-core", queries[0].Name)
	assert.Equal(t, "6.1.3", queries[0].Version)
}

func TestParseMavenDeps_EmptyPom(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseMavenDeps_MalformedXML(t *testing.T) {
	pom := []byte(`this is not valid XML at all <<<<`)

	_, err := parseMavenDeps(pom)
	assert.Error(t, err)
}

func TestParseMavenDeps_EmptyInput(t *testing.T) {
	_, err := parseMavenDeps([]byte{})
	assert.Error(t, err)
}

func TestParseMavenDeps_MinimalProject_NoDeps(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseMavenDeps_DuplicateDependency_Deduped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.google.guava</groupId>
        <artifactId>guava</artifactId>
        <version>31.1-jre</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
  <dependencies>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>32.0-jre</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	// The <dependencies> entry appears first in allDeps, so it wins.
	require.Len(t, queries, 1)
	assert.Equal(t, "com.google.guava:guava", queries[0].Name)
	assert.Equal(t, "32.0-jre", queries[0].Version)
}

func TestParseMavenDeps_NonProductionScopes_Kept(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>compile-dep</artifactId>
      <version>1.0.0</version>
      <scope>compile</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>runtime-dep</artifactId>
      <version>2.0.0</version>
      <scope>runtime</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>provided-dep</artifactId>
      <version>3.0.0</version>
      <scope>provided</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>system-dep</artifactId>
      <version>4.0.0</version>
      <scope>system</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>no-scope-dep</artifactId>
      <version>5.0.0</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	assert.Len(t, queries, 5)
}

func TestParseMavenDeps_ProjectGroupIdProperty(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <properties>
    <lib.version>3.0.0</lib.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>internal-lib</artifactId>
      <version>${lib.version}</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "com.example:internal-lib", queries[0].Name)
	assert.Equal(t, "3.0.0", queries[0].Version)
}

func TestParseMavenDeps_RealWorldPom(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <groupId>com.example</groupId>
  <artifactId>webapp</artifactId>
  <version>3.2.1</version>
  <packaging>jar</packaging>

  <properties>
    <java.version>17</java.version>
    <spring-boot.version>3.2.2</spring-boot.version>
    <lombok.version>1.18.30</lombok.version>
    <mapstruct.version>1.5.5.Final</mapstruct.version>
  </properties>

  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.springframework.boot</groupId>
        <artifactId>spring-boot-dependencies</artifactId>
        <version>${spring-boot.version}</version>
        <type>pom</type>
        <scope>import</scope>
      </dependency>
    </dependencies>
  </dependencyManagement>

  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
      <version>${spring-boot.version}</version>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
      <version>${spring-boot.version}</version>
    </dependency>
    <dependency>
      <groupId>org.projectlombok</groupId>
      <artifactId>lombok</artifactId>
      <version>${lombok.version}</version>
      <scope>provided</scope>
    </dependency>
    <dependency>
      <groupId>org.mapstruct</groupId>
      <artifactId>mapstruct</artifactId>
      <version>${mapstruct.version}</version>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>internal-common</artifactId>
      <version>${project.version}</version>
    </dependency>
    <dependency>
      <groupId>com.h2database</groupId>
      <artifactId>h2</artifactId>
      <version>2.2.224</version>
      <scope>test</scope>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-test</artifactId>
      <version>${spring-boot.version}</version>
      <scope>test</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>parent-managed</artifactId>
      <version>${parent.version}</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)

	byName := make(map[string]string)
	for _, q := range queries {
		byName[q.Name] = q.Version
		assert.Equal(t, "Maven", q.Ecosystem)
	}

	// Dependencies section: spring-boot-starter-web, spring-boot-starter-data-jpa, lombok,
	// mapstruct, internal-common resolved via ${project.version}.
	assert.Equal(t, "3.2.2", byName["org.springframework.boot:spring-boot-starter-web"])
	assert.Equal(t, "3.2.2", byName["org.springframework.boot:spring-boot-starter-data-jpa"])
	assert.Equal(t, "1.18.30", byName["org.projectlombok:lombok"])
	assert.Equal(t, "1.5.5.Final", byName["org.mapstruct:mapstruct"])
	assert.Equal(t, "3.2.1", byName["com.example:internal-common"])

	// DependencyManagement section: spring-boot-dependencies (deduplicated with spring-boot- prefix names).
	assert.Equal(t, "3.2.2", byName["org.springframework.boot:spring-boot-dependencies"])

	// Test-scoped dependencies should be excluded.
	_, hasH2 := byName["com.h2database:h2"]
	assert.False(t, hasH2, "test-scoped h2 should be skipped")
	_, hasTestStarter := byName["org.springframework.boot:spring-boot-starter-test"]
	assert.False(t, hasTestStarter, "test-scoped spring-boot-starter-test should be skipped")

	// Unresolvable property: ${parent.version} is not defined in this pom.
	_, hasParentManaged := byName["com.example:parent-managed"]
	assert.False(t, hasParentManaged, "dep with unresolvable ${parent.version} should be skipped")

	// Total: 5 from dependencies + 1 from dependencyManagement = 6
	assert.Len(t, queries, 6)
}

func TestParseMavenDeps_WhitespaceInElements(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>  org.apache.commons  </groupId>
      <artifactId>  commons-lang3  </artifactId>
      <version>  3.12.0  </version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "org.apache.commons:commons-lang3", queries[0].Name)
	assert.Equal(t, "3.12.0", queries[0].Version)
}

func TestParseMavenDeps_MissingGroupId_Skipped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <artifactId>no-group</artifactId>
      <version>1.0.0</version>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>valid</artifactId>
      <version>2.0.0</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "com.example:valid", queries[0].Name)
}

func TestParseMavenDeps_MissingArtifactId_Skipped(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <version>1.0.0</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestResolveProperties(t *testing.T) {
	props := map[string]string{
		"spring.version": "6.1.3",
		"java.version":   "17",
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no placeholder", "3.12.0", "3.12.0"},
		{"single placeholder", "${spring.version}", "6.1.3"},
		{"undefined placeholder", "${unknown.prop}", "${unknown.prop}"},
		{"empty string", "", ""},
		{"partial match", "prefix-${spring.version}-suffix", "prefix-6.1.3-suffix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveProperties(tt.input, props))
		})
	}
}

func TestBuildPropertyMap(t *testing.T) {
	project := &pomProject{
		GroupID:    "com.example",
		ArtifactID: "myapp",
		Version:    "2.0.0",
		Properties: pomProperties{
			Entries: []pomProperty{
				{XMLName: xmlName("spring.version"), Value: "6.1.3"},
				{XMLName: xmlName("custom.prop"), Value: "  trimmed  "},
			},
		},
	}

	props := buildPropertyMap(project)

	assert.Equal(t, "2.0.0", props["project.version"])
	assert.Equal(t, "com.example", props["project.groupId"])
	assert.Equal(t, "myapp", props["project.artifactId"])
	assert.Equal(t, "6.1.3", props["spring.version"])
	assert.Equal(t, "trimmed", props["custom.prop"])
}

func TestBuildPropertyMap_EmptyProject(t *testing.T) {
	project := &pomProject{}
	props := buildPropertyMap(project)

	// No built-in properties when project fields are empty.
	_, hasVersion := props["project.version"]
	assert.False(t, hasVersion)
	_, hasGroupID := props["project.groupId"]
	assert.False(t, hasGroupID)
	_, hasArtifactID := props["project.artifactId"]
	assert.False(t, hasArtifactID)
}

func TestParseMavenDeps_TestScopeInDependencyManagement(t *testing.T) {
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>junit</groupId>
        <artifactId>junit</artifactId>
        <version>4.13.2</version>
        <scope>test</scope>
      </dependency>
      <dependency>
        <groupId>org.apache.commons</groupId>
        <artifactId>commons-lang3</artifactId>
        <version>3.12.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)

	assert.Equal(t, "org.apache.commons:commons-lang3", queries[0].Name)
}

func TestParseMavenDeps_NamespacePrefix(t *testing.T) {
	// pom.xml files often have a namespace; ensure it parses correctly.
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.apache.commons</groupId>
      <artifactId>commons-lang3</artifactId>
      <version>3.12.0</version>
    </dependency>
  </dependencies>
</project>`)

	queries, err := parseMavenDeps(pom)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "org.apache.commons:commons-lang3", queries[0].Name)
}

// xmlName is a test helper to create xml.Name with a local name.
func xmlName(local string) xml.Name {
	return xml.Name{Local: local}
}
