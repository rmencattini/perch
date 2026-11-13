package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxCompletions = 50

// expandHome turns "~" and "~/x" into absolute paths. Anything else is unchanged.
func expandHome(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return p
}

// Completions lists the sub-folders that continue input. Results keep the
// user's own spelling ("~/g" -> "~/git/"), so they can feed textinput suggestions.
func Completions(input, home string) []string {
	if input == "" {
		return nil
	}
	parent, prefix := input, ""
	if !strings.HasSuffix(input, "/") {
		i := strings.LastIndex(input, "/")
		if i < 0 {
			return nil
		}
		parent, prefix = input[:i+1], input[i+1:]
	}
	ents, err := os.ReadDir(expandHome(parent, home))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		if fi, err := os.Stat(filepath.Join(expandHome(parent, home), name)); err != nil || !fi.IsDir() {
			continue // also resolves symlinks to folders
		}
		out = append(out, parent+name+"/")
	}
	sort.Strings(out)
	if len(out) > maxCompletions {
		out = out[:maxCompletions]
	}
	return out
}

// tildify shortens a path under home to ~/... for display.
func tildify(p, home string) string {
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
