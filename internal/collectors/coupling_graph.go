// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"regexp"
	"strings"
)

// importGraph maps a module name to the set of modules it imports.
type importGraph map[string][]string

// --- Per-language import extraction regexes ---

// Go: import "path" or import ( "path" ... )
var goImportSingle = regexp.MustCompile(`^\s*import\s+"([^"]+)"`)
var goImportGroupLine = regexp.MustCompile(`^\s*(?:\w+\s+)?"([^"]+)"`)

// JS/TS: import ... from 'path' or require('path')
var jsImportFrom = regexp.MustCompile(`(?:import|export)\s+.*?from\s+['"]([^'"]+)['"]`)
var jsRequire = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)

// Python: import module or from module import ...
var pyImport = regexp.MustCompile(`^\s*import\s+([\w.]+)`)
var pyFromImport = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import`)

// Java: import com.example.package.Class
var javaImport = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.]+)`)

// Rust: use crate::module::item
var rustUse = regexp.MustCompile(`^\s*(?:pub\s+)?use\s+crate::(\w+)`)

// Ruby: require_relative './path'
var rubyRequireRelative = regexp.MustCompile(`^\s*require_relative\s+['"]([^'"]+)['"]`)

// PHP: use Namespace\Class
var phpUse = regexp.MustCompile(`^\s*use\s+([\w\\]+)`)

// C/C++: #include "path" (project-local only, not <system>)
var cLocalInclude = regexp.MustCompile(`^\s*#\s*include\s+"([^"]+)"`)

// importExtractor maps file extensions to their extraction function.
type importExtractor func(lines []string, relPath string, modulePath string, allModules map[string]bool) []string

var importExtractors = map[string]importExtractor{
	".go":   extractGoImports,
	".js":   extractJSImports,
	".ts":   extractJSImports,
	".jsx":  extractJSImports,
	".tsx":  extractJSImports,
	".py":   extractPythonImports,
	".java": extractJavaImports,
	".rs":   extractRustImports,
	".rb":   extractRubyImports,
	".php":  extractPHPImports,
	".c":    extractCImports,
	".cpp":  extractCImports,
	".h":    extractCImports,
	".hpp":  extractCImports,
}

// extractGoImports extracts Go package imports, filtering to intra-project only.
func extractGoImports(lines []string, relPath string, modulePath string, allModules map[string]bool) []string {
	var imports []string
	inGroup := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "import (") {
			inGroup = true
			continue
		}
		if inGroup && trimmed == ")" {
			inGroup = false
			continue
		}

		var match []string
		if inGroup {
			match = goImportGroupLine.FindStringSubmatch(line)
		} else {
			match = goImportSingle.FindStringSubmatch(line)
		}

		if match == nil {
			continue
		}

		importPath := match[1]

		// Only track intra-project imports.
		if modulePath == "" || !strings.HasPrefix(importPath, modulePath+"/") {
			continue
		}

		// Convert to repo-relative package directory.
		pkgDir := strings.TrimPrefix(importPath, modulePath+"/")
		if allModules[pkgDir] {
			imports = append(imports, pkgDir)
		}
	}

	return imports
}

// extractJSImports extracts JS/TS import and require statements.
func extractJSImports(lines []string, relPath string, _ string, allModules map[string]bool) []string {
	var imports []string
	dir := filepath.Dir(relPath)

	for _, line := range lines {
		var importPath string
		if m := jsImportFrom.FindStringSubmatch(line); m != nil {
			importPath = m[1]
		} else if m := jsRequire.FindStringSubmatch(line); m != nil {
			importPath = m[1]
		}

		if importPath == "" {
			continue
		}

		// Skip external/node_modules imports (bare specifiers).
		if !strings.HasPrefix(importPath, ".") {
			continue
		}

		// Resolve relative to the importing file's directory.
		resolved := filepath.Clean(filepath.Join(dir, importPath))
		resolved = strings.TrimPrefix(resolved, "./")

		// Strip known extensions for matching.
		for _, ext := range []string{".js", ".ts", ".jsx", ".tsx"} {
			resolved = strings.TrimSuffix(resolved, ext)
		}

		// Check if the resolved path matches any known module.
		if allModules[resolved] {
			imports = append(imports, resolved)
		}
		// Also try with /index stripped (common JS convention).
		withIndex := resolved + "/index"
		if allModules[withIndex] {
			imports = append(imports, withIndex)
		}
	}

	return imports
}

// extractPythonImports extracts Python import and from...import statements.
func extractPythonImports(lines []string, _ string, _ string, allModules map[string]bool) []string {
	var imports []string

	for _, line := range lines {
		var moduleName string
		if m := pyFromImport.FindStringSubmatch(line); m != nil {
			moduleName = m[1]
		} else if m := pyImport.FindStringSubmatch(line); m != nil {
			moduleName = m[1]
		}

		if moduleName == "" {
			continue
		}

		// Check full dotted name and parent packages.
		if allModules[moduleName] {
			imports = append(imports, moduleName)
		} else {
			// Try parent module (e.g., "myapp.models.user" → "myapp.models").
			parts := strings.Split(moduleName, ".")
			for i := len(parts) - 1; i >= 1; i-- {
				parent := strings.Join(parts[:i], ".")
				if allModules[parent] {
					imports = append(imports, parent)
					break
				}
			}
		}
	}

	return imports
}

// extractJavaImports extracts Java import statements, keeping package portion.
func extractJavaImports(lines []string, _ string, _ string, allModules map[string]bool) []string {
	var imports []string

	for _, line := range lines {
		m := javaImport.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		// Strip the class name, keep the package.
		full := m[1]
		if idx := strings.LastIndex(full, "."); idx > 0 {
			pkg := full[:idx]
			if allModules[pkg] {
				imports = append(imports, pkg)
			}
		}
	}

	return imports
}

// extractRustImports extracts Rust crate-local use statements.
func extractRustImports(lines []string, _ string, _ string, allModules map[string]bool) []string {
	var imports []string

	for _, line := range lines {
		m := rustUse.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		mod := m[1]
		if allModules[mod] {
			imports = append(imports, mod)
		}
	}

	return imports
}

// extractRubyImports extracts Ruby require_relative statements.
func extractRubyImports(lines []string, relPath string, _ string, allModules map[string]bool) []string {
	var imports []string
	dir := filepath.Dir(relPath)

	for _, line := range lines {
		m := rubyRequireRelative.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		resolved := filepath.Clean(filepath.Join(dir, m[1]))
		resolved = strings.TrimPrefix(resolved, "./")
		resolved = strings.TrimSuffix(resolved, ".rb")

		if allModules[resolved] {
			imports = append(imports, resolved)
		}
	}

	return imports
}

// extractPHPImports extracts PHP use statements, keeping namespace.
func extractPHPImports(lines []string, _ string, _ string, allModules map[string]bool) []string {
	var imports []string

	for _, line := range lines {
		m := phpUse.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		full := m[1]
		// Strip the class name, keep the namespace.
		if idx := strings.LastIndex(full, `\`); idx > 0 {
			ns := full[:idx]
			if allModules[ns] {
				imports = append(imports, ns)
			}
		}
	}

	return imports
}

// extractCImports extracts project-local #include "..." statements.
func extractCImports(lines []string, _ string, _ string, allModules map[string]bool) []string {
	var imports []string

	for _, line := range lines {
		m := cLocalInclude.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		includePath := m[1]
		if allModules[includePath] {
			imports = append(imports, includePath)
		}
	}

	return imports
}

// --- Module identity resolution ---

// moduleForFile returns the module identity for a file given its relative path
// and extension. This determines how files are grouped into "modules" for
// graph construction.
func moduleForFile(relPath string, ext string) string {
	switch ext {
	case ".go":
		// Go: package directory path.
		return filepath.Dir(relPath)
	case ".js", ".ts", ".jsx", ".tsx":
		// JS/TS: file path without extension.
		return strings.TrimSuffix(relPath, ext)
	case ".py":
		// Python: dotted module name from path.
		noExt := strings.TrimSuffix(relPath, ext)
		return strings.ReplaceAll(filepath.ToSlash(noExt), "/", ".")
	case ".java":
		// Java: extract package from directory structure.
		return strings.ReplaceAll(filepath.ToSlash(filepath.Dir(relPath)), "/", ".")
	case ".rs":
		// Rust: crate-local module path (first component after src/).
		parts := strings.Split(filepath.ToSlash(relPath), "/")
		for i, p := range parts {
			if p == "src" && i+1 < len(parts) {
				name := strings.TrimSuffix(parts[i+1], ".rs")
				if name == "main" || name == "lib" {
					return "crate"
				}
				return name
			}
		}
		return strings.TrimSuffix(filepath.Base(relPath), ".rs")
	case ".rb":
		// Ruby: file path without extension.
		return strings.TrimSuffix(relPath, ext)
	case ".php":
		// PHP: namespace from directory structure using backslash convention.
		dir := filepath.Dir(relPath)
		// Convert path separators to backslash for PHP namespace.
		return strings.ReplaceAll(dir, string(filepath.Separator), `\`)
	case ".c", ".cpp", ".h", ".hpp":
		// C/C++: include path (the file's relative path).
		return relPath
	}
	return relPath
}

// --- Tarjan's SCC algorithm ---

// tarjanSCC finds all strongly connected components in the graph.
// Returns only components with 2+ nodes (actual cycles).
func tarjanSCC(graph importGraph) [][]string {
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowlinks := map[string]int{}
	var sccs [][]string

	var strongConnect func(v string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range graph[v] {
			if _, visited := indices[w]; !visited {
				strongConnect(w)
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		// Root of an SCC.
		if lowlinks[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			// Only keep cycles (2+ nodes).
			if len(scc) >= 2 {
				sccs = append(sccs, scc)
			}
		}
	}

	// Visit all nodes, including those with no outgoing edges.
	for v := range graph {
		if _, visited := indices[v]; !visited {
			strongConnect(v)
		}
	}

	return sccs
}

// --- Fan-out analysis ---

const defaultFanOutThreshold = 10

// fanOutModules returns modules whose direct import count meets or exceeds
// the threshold, along with their counts.
func fanOutModules(graph importGraph, threshold int) map[string]int {
	results := make(map[string]int)
	for mod, deps := range graph {
		// Deduplicate imports.
		unique := make(map[string]bool)
		for _, d := range deps {
			unique[d] = true
		}
		count := len(unique)
		if count >= threshold {
			results[mod] = count
		}
	}
	return results
}

// readGoModulePath reads the module path from a go.mod file.
func readGoModulePath(repoPath string) string {
	goModPath := filepath.Join(repoPath, "go.mod")
	lines, err := readFileLines(goModPath)
	if err != nil {
		return ""
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "module"))
		}
	}
	return ""
}
