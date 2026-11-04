package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePrefix = "go.airlance.org/server/internal/"

var allowed = map[string][]string{
	"domain":         {"domain"},
	"usecase":        {"domain", "usecase"},
	"repository":     {"domain", "usecase"},
	"infrastructure": {"domain", "usecase", "infrastructure"},
	"transport":      {"domain", "usecase", "transport"},
	"bootstrap":      {"domain", "usecase", "repository", "infrastructure", "transport"},
}

func TestLayersOnlyDependInward(t *testing.T) {
	t.Parallel()

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}

		layer, _, found := strings.Cut(filepath.ToSlash(path), "/")
		if !found {
			return nil
		}

		rules, known := allowed[layer]
		if !known {
			t.Errorf("%s: %q is not a known layer", path, layer)

			return nil
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, spec := range parsed.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}

			target, inside := strings.CutPrefix(name, modulePrefix)
			if !inside {
				continue
			}

			targetLayer, _, _ := strings.Cut(target, "/")
			if !contains(rules, targetLayer) {
				t.Errorf("%s: %s must not import %s", path, layer, targetLayer)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat("domain"); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}

	return false
}
