package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGradleDeps_GroovyStringNotation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []PackageQuery
	}{
		{
			name:  "single quotes",
			input: "implementation 'com.google.guava:guava:31.1-jre'",
			want: []PackageQuery{
				{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
			},
		},
		{
			name:  "double quotes",
			input: `implementation "org.springframework:spring-core:5.3.20"`,
			want: []PackageQuery{
				{Ecosystem: "Maven", Name: "org.springframework:spring-core", Version: "5.3.20"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGradleDeps([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGradleDeps_KotlinDSL(t *testing.T) {
	input := `implementation("com.google.guava:guava:31.1-jre")
api("io.netty:netty-all:4.1.85.Final")`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, PackageQuery{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"}, got[0])
	assert.Equal(t, PackageQuery{Ecosystem: "Maven", Name: "io.netty:netty-all", Version: "4.1.85.Final"}, got[1])
}

func TestParseGradleDeps_MapNotation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []PackageQuery
	}{
		{
			name:  "single quotes",
			input: "implementation group: 'com.google.guava', name: 'guava', version: '31.1-jre'",
			want: []PackageQuery{
				{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
			},
		},
		{
			name:  "double quotes",
			input: `implementation group: "org.apache.commons", name: "commons-lang3", version: "3.12.0"`,
			want: []PackageQuery{
				{Ecosystem: "Maven", Name: "org.apache.commons:commons-lang3", Version: "3.12.0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGradleDeps([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGradleDeps_AllConfigurations(t *testing.T) {
	input := `implementation 'com.example:impl:1.0'
api 'com.example:api-lib:2.0'
compile 'com.example:compile-lib:3.0'
runtimeOnly 'com.example:runtime-lib:4.0'
compileOnly 'com.example:compileonly-lib:5.0'
classpath 'com.example:classpath-lib:6.0'`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 6)

	names := make([]string, len(got))
	for i, q := range got {
		names[i] = q.Name
	}

	assert.Contains(t, names, "com.example:impl")
	assert.Contains(t, names, "com.example:api-lib")
	assert.Contains(t, names, "com.example:compile-lib")
	assert.Contains(t, names, "com.example:runtime-lib")
	assert.Contains(t, names, "com.example:compileonly-lib")
	assert.Contains(t, names, "com.example:classpath-lib")

	// All should be Maven ecosystem.
	for _, q := range got {
		assert.Equal(t, "Maven", q.Ecosystem)
	}
}

func TestParseGradleDeps_TestConfigsSkipped(t *testing.T) {
	input := `implementation 'com.example:prod-lib:1.0'
testImplementation 'junit:junit:4.13.2'
testCompileOnly 'org.projectlombok:lombok:1.18.24'
testRuntimeOnly 'org.junit.jupiter:junit-jupiter-engine:5.9.1'`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "com.example:prod-lib", got[0].Name)
}

func TestParseGradleDeps_NoVersion(t *testing.T) {
	input := `implementation 'com.google.guava:guava'
implementation 'com.example:has-version:1.0'`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "com.example:has-version", got[0].Name)
	assert.Equal(t, "1.0", got[0].Version)
}

func TestParseGradleDeps_MapNotationNoVersion(t *testing.T) {
	input := "implementation group: 'com.google.guava', name: 'guava'"

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseGradleDeps_CommentsAndNonDepLines(t *testing.T) {
	input := `// This is a comment
plugins {
    id 'java'
}

repositories {
    mavenCentral()
}

dependencies {
    // Production dependency
    implementation 'com.google.guava:guava:31.1-jre'
    // api 'com.example:commented-out:1.0'
    // testImplementation 'junit:junit:4.13.2'
}

task clean(type: Delete) {
    delete rootProject.buildDir
}`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "com.google.guava:guava", got[0].Name)
	assert.Equal(t, "31.1-jre", got[0].Version)
}

func TestParseGradleDeps_EmptyFile(t *testing.T) {
	got, err := parseGradleDeps([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseGradleDeps_NilInput(t *testing.T) {
	got, err := parseGradleDeps(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseGradleDeps_RealWorldBuildGradle(t *testing.T) {
	input := `plugins {
    id 'java'
    id 'org.springframework.boot' version '3.1.0'
    id 'io.spring.dependency-management' version '1.1.0'
}

group = 'com.example'
version = '0.0.1-SNAPSHOT'
sourceCompatibility = '17'

repositories {
    mavenCentral()
    maven { url 'https://repo.spring.io/milestone' }
}

dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web:3.1.0'
    implementation 'org.springframework.boot:spring-boot-starter-data-jpa:3.1.0'
    runtimeOnly 'org.postgresql:postgresql:42.6.0'
    compileOnly 'org.projectlombok:lombok:1.18.28'
    api 'com.google.guava:guava:32.1.2-jre'

    testImplementation 'org.springframework.boot:spring-boot-starter-test:3.1.0'
    testRuntimeOnly 'org.junit.platform:junit-platform-launcher:1.9.3'
}

tasks.named('test') {
    useJUnitPlatform()
}`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 5)

	names := make(map[string]string)
	for _, q := range got {
		names[q.Name] = q.Version
	}

	assert.Equal(t, "3.1.0", names["org.springframework.boot:spring-boot-starter-web"])
	assert.Equal(t, "3.1.0", names["org.springframework.boot:spring-boot-starter-data-jpa"])
	assert.Equal(t, "42.6.0", names["org.postgresql:postgresql"])
	assert.Equal(t, "1.18.28", names["org.projectlombok:lombok"])
	assert.Equal(t, "32.1.2-jre", names["com.google.guava:guava"])

	// Test deps should not be present.
	_, hasTestWeb := names["org.springframework.boot:spring-boot-starter-test"]
	assert.False(t, hasTestWeb, "testImplementation deps should be skipped")
	_, hasTestPlatform := names["org.junit.platform:junit-platform-launcher"]
	assert.False(t, hasTestPlatform, "testRuntimeOnly deps should be skipped")
}

func TestParseGradleDeps_RealWorldBuildGradleKts(t *testing.T) {
	input := `plugins {
    java
    id("org.springframework.boot") version "3.1.0"
}

group = "com.example"
version = "1.0.0"

repositories {
    mavenCentral()
}

dependencies {
    implementation("org.springframework.boot:spring-boot-starter-web:3.1.0")
    implementation("com.fasterxml.jackson.core:jackson-databind:2.15.2")
    runtimeOnly("com.h2database:h2:2.1.214")
    testImplementation("org.springframework.boot:spring-boot-starter-test:3.1.0")
}`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 3)

	names := make(map[string]string)
	for _, q := range got {
		names[q.Name] = q.Version
	}

	assert.Equal(t, "3.1.0", names["org.springframework.boot:spring-boot-starter-web"])
	assert.Equal(t, "2.15.2", names["com.fasterxml.jackson.core:jackson-databind"])
	assert.Equal(t, "2.1.214", names["com.h2database:h2"])
}

func TestParseGradleDeps_DuplicateDependencies(t *testing.T) {
	input := `implementation 'com.google.guava:guava:31.1-jre'
implementation 'com.google.guava:guava:31.1-jre'`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 1, "duplicate dependencies should be deduplicated")
}

func TestParseGradleDeps_MixedNotations(t *testing.T) {
	input := `implementation 'com.google.guava:guava:31.1-jre'
api("io.netty:netty-all:4.1.85.Final")
compile group: 'org.apache.commons', name: 'commons-lang3', version: '3.12.0'`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 3)

	names := make(map[string]string)
	for _, q := range got {
		names[q.Name] = q.Version
	}

	assert.Equal(t, "31.1-jre", names["com.google.guava:guava"])
	assert.Equal(t, "4.1.85.Final", names["io.netty:netty-all"])
	assert.Equal(t, "3.12.0", names["org.apache.commons:commons-lang3"])
}

func TestParseGradleDeps_IndentedLines(t *testing.T) {
	input := `dependencies {
    implementation 'com.google.guava:guava:31.1-jre'
        api "io.netty:netty-all:4.1.85.Final"
	runtimeOnly 'com.example:tabbed:1.0'
}`

	got, err := parseGradleDeps([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestParseCoordinates(t *testing.T) {
	tests := []struct {
		name   string
		coords string
		want   *PackageQuery
	}{
		{
			name:   "valid three-part",
			coords: "com.google.guava:guava:31.1-jre",
			want:   &PackageQuery{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
		},
		{
			name:   "four-part with classifier",
			coords: "com.google.guava:guava:31.1-jre:sources",
			want:   &PackageQuery{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
		},
		{
			name:   "two parts no version",
			coords: "com.google.guava:guava",
			want:   nil,
		},
		{
			name:   "empty version",
			coords: "com.google.guava:guava:",
			want:   nil,
		},
		{
			name:   "single part",
			coords: "guava",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCoordinates(tt.coords)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseMapNotation(t *testing.T) {
	tests := []struct {
		name string
		line string
		want *PackageQuery
	}{
		{
			name: "single quotes",
			line: "implementation group: 'com.google.guava', name: 'guava', version: '31.1-jre'",
			want: &PackageQuery{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
		},
		{
			name: "double quotes",
			line: `implementation group: "com.google.guava", name: "guava", version: "31.1-jre"`,
			want: &PackageQuery{Ecosystem: "Maven", Name: "com.google.guava:guava", Version: "31.1-jre"},
		},
		{
			name: "missing version",
			line: "implementation group: 'com.google.guava', name: 'guava'",
			want: nil,
		},
		{
			name: "missing name",
			line: "implementation group: 'com.google.guava', version: '31.0'",
			want: nil,
		},
		{
			name: "missing group",
			line: "implementation name: 'guava', version: '31.0'",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMapNotation(tt.line)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsTestConfig(t *testing.T) {
	assert.True(t, isTestConfig("testimplementation"))
	assert.True(t, isTestConfig("testcompileonly"))
	assert.True(t, isTestConfig("testruntimeonly"))
	assert.False(t, isTestConfig("implementation"))
	assert.False(t, isTestConfig("api"))
	assert.False(t, isTestConfig(""))
}

func TestExtractConfig(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"implementation 'com.google:guava:31.0'", "implementation"},
		{"  api 'com.example:lib:1.0'", "api"},
		{"testImplementation 'junit:junit:4.13.2'", "testimplementation"},
		{"compile group: 'com.example', name: 'lib', version: '1.0'", "compile"},
		{"nothing here", ""},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := extractConfig(tt.line)
			assert.Equal(t, tt.want, got)
		})
	}
}
