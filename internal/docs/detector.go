// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package docs

import (
	"bufio"
	"path/filepath"
	"regexp"
	"strings"
)

// Detection holds what was detected from a single build file.
type Detection struct {
	File       string
	Components []TechComponent
	Commands   []BuildCommand
}

// Detector checks for a specific build file and extracts info.
type Detector struct {
	File   string // filename to look for, e.g., "go.mod"
	Detect func(repoPath string) *Detection
}

// builtinDetectors is the set of build file detectors.
var builtinDetectors = []Detector{
	{File: "go.mod", Detect: detectGoMod},
	{File: "package.json", Detect: detectPackageJSON},
	{File: "Makefile", Detect: detectMakefile},
	{File: "Cargo.toml", Detect: detectCargoToml},
	{File: "pyproject.toml", Detect: detectPyprojectToml},
	{File: "requirements.txt", Detect: detectRequirementsTxt},
	{File: "Dockerfile", Detect: detectDockerfile},
	{File: ".goreleaser.yml", Detect: detectGoReleaser},
	{File: ".goreleaser.yaml", Detect: detectGoReleaser},
}

// DetectAll runs all detectors against the repo and returns findings.
func DetectAll(repoPath string) []Detection {
	var results []Detection
	for _, d := range builtinDetectors {
		if _, err := FS.Stat(filepath.Join(repoPath, d.File)); err == nil {
			if det := d.Detect(repoPath); det != nil {
				results = append(results, *det)
			}
		}
	}
	return results
}

func detectGoMod(repoPath string) *Detection {
	path := filepath.Join(repoPath, "go.mod")
	f, err := FS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	det := &Detection{File: "go.mod"}

	scanner := bufio.NewScanner(f)
	goVersionRe := regexp.MustCompile(`^go\s+(\d+\.\d+)`)

	inRequire := false
	for scanner.Scan() {
		line := scanner.Text()

		if matches := goVersionRe.FindStringSubmatch(line); matches != nil {
			det.Components = append(det.Components, TechComponent{
				Name: "Go", Version: matches[1], Source: "go.mod",
			})
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require (") || strings.HasPrefix(trimmed, "require(") {
			inRequire = true
			continue
		}
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}

		depLine := line
		if !inRequire {
			if strings.HasPrefix(trimmed, "require ") {
				depLine = strings.TrimPrefix(trimmed, "require ")
			} else {
				continue
			}
		}

		depLine = strings.TrimSpace(depLine)
		if strings.Contains(depLine, " v") {
			switch {
			case strings.Contains(depLine, "spf13/cobra"):
				det.Components = append(det.Components, TechComponent{
					Name: "Cobra", Version: "", Source: "go.mod",
				})
			case strings.Contains(depLine, "stretchr/testify"):
				det.Components = append(det.Components, TechComponent{
					Name: "Testify", Version: "", Source: "go.mod",
				})
			case strings.Contains(depLine, "go-git"):
				det.Components = append(det.Components, TechComponent{
					Name: "go-git", Version: "", Source: "go.mod",
				})
			}
		}
	}

	det.Commands = []BuildCommand{
		{Name: "build", Command: "go build ./...", Source: "go.mod"},
		{Name: "test", Command: "go test -race ./...", Source: "go.mod"},
		{Name: "vet", Command: "go vet ./...", Source: "go.mod"},
	}

	return det
}

func detectPackageJSON(repoPath string) *Detection {
	path := filepath.Join(repoPath, "package.json")
	if _, err := FS.Stat(path); err != nil {
		return nil
	}

	return &Detection{
		File: "package.json",
		Components: []TechComponent{
			{Name: "Node.js", Version: "", Source: "package.json"},
		},
		Commands: []BuildCommand{
			{Name: "install", Command: "npm install", Source: "package.json"},
			{Name: "test", Command: "npm test", Source: "package.json"},
		},
	}
}

func detectMakefile(repoPath string) *Detection {
	path := filepath.Join(repoPath, "Makefile")
	f, err := FS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	det := &Detection{File: "Makefile"}
	targetRe := regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*):\s*`)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := targetRe.FindStringSubmatch(line); matches != nil {
			target := matches[1]
			switch target {
			case "build", "test", "lint", "clean", "install", "run", "dev", "all", "check", "fmt", "format":
				det.Commands = append(det.Commands, BuildCommand{
					Name: target, Command: "make " + target, Source: "Makefile",
				})
			}
		}
	}

	return det
}

func detectCargoToml(repoPath string) *Detection {
	return &Detection{
		File: "Cargo.toml",
		Components: []TechComponent{
			{Name: "Rust", Version: "", Source: "Cargo.toml"},
		},
		Commands: []BuildCommand{
			{Name: "build", Command: "cargo build", Source: "Cargo.toml"},
			{Name: "test", Command: "cargo test", Source: "Cargo.toml"},
			{Name: "check", Command: "cargo check", Source: "Cargo.toml"},
		},
	}
}

func detectPyprojectToml(repoPath string) *Detection {
	return &Detection{
		File: "pyproject.toml",
		Components: []TechComponent{
			{Name: "Python", Version: "", Source: "pyproject.toml"},
		},
		Commands: []BuildCommand{
			{Name: "test", Command: "pytest", Source: "pyproject.toml"},
		},
	}
}

func detectRequirementsTxt(repoPath string) *Detection {
	return &Detection{
		File: "requirements.txt",
		Components: []TechComponent{
			{Name: "Python", Version: "", Source: "requirements.txt"},
		},
		Commands: []BuildCommand{
			{Name: "install", Command: "pip install -r requirements.txt", Source: "requirements.txt"},
		},
	}
}

func detectDockerfile(repoPath string) *Detection {
	return &Detection{
		File: "Dockerfile",
		Components: []TechComponent{
			{Name: "Docker", Version: "", Source: "Dockerfile"},
		},
		Commands: []BuildCommand{
			{Name: "build", Command: "docker build .", Source: "Dockerfile"},
		},
	}
}

func detectGoReleaser(_ string) *Detection {
	return &Detection{
		File: ".goreleaser.yml",
		Components: []TechComponent{
			{Name: "GoReleaser", Version: "", Source: ".goreleaser.yml"},
		},
	}
}
