package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoEmDashes is a house style rule the owner asked for: eco's own voice
// never uses an em dash (U+2014), in the interface, the documentation or the source.
// Test fixtures are exempt: internal/render/testdata carries an em dash as
// sample Unicode for the CJK and RTL corpus, and rewriting it would change
// what the frozen golden images render.
const emDash = '\u2014'

func TestNoEmDashes(t *testing.T) {
	root := "../.."
	skip := map[string]bool{
		".git": true, "v1": true, "iv1": true, "dist": true, "testdata": true,
	}
	var bad []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.ContainsRune(line, emDash) {
				rel, _ := filepath.Rel(root, path)
				bad = append(bad, rel+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Errorf("em dash found in %d place(s); eco's voice does not use them:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}
