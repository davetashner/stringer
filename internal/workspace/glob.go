package workspace

import (
	"path/filepath"
	"sort"
)

// expandGlobs resolves a list of glob patterns relative to root into absolute
// directory paths. Non-directory matches are silently skipped. Results are
// sorted and deduplicated.
func expandGlobs(root string, patterns []string) ([]string, error) {
	seen := make(map[string]bool)
	var dirs []string

	for _, pat := range patterns {
		abs := pat
		if !filepath.IsAbs(pat) {
			abs = filepath.Join(root, pat)
		}

		matches, err := filepath.Glob(abs)
		if err != nil {
			return nil, err
		}

		for _, m := range matches {
			if seen[m] {
				continue
			}
			if dirExists(m) {
				seen[m] = true
				dirs = append(dirs, m)
			}
		}
	}

	sort.Strings(dirs)
	return dirs, nil
}

// dirsToWorkspaces converts absolute directory paths into Workspace structs
// relative to root, using the directory basename as the workspace name.
func dirsToWorkspaces(root string, dirs []string) []Workspace {
	ws := make([]Workspace, 0, len(dirs))
	for _, d := range dirs {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			rel = filepath.Base(d)
		}
		ws = append(ws, Workspace{
			Name: filepath.Base(d),
			Path: d,
			Rel:  rel,
		})
	}
	return ws
}
