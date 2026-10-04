package httpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestBackendApplicationMessagesUseEnglish(t *testing.T) {
	// Check application-owned literals, not user input, upstream metadata, or old persisted events.
	for _, dir := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				// Explicit locale-map values are UI metadata, not diagnostic messages.
				if pair, ok := node.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
						if locale, _ := strconv.Unquote(key.Value); locale == "zh-CN" {
							return false
						}
					}
				}
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				// This is an input parser for legacy event history, never a newly emitted message.
				if path == "../../internal/store/store.go" && value == "\u4e0a\u6e38 HTTP %d" {
					return true
				}
				for _, r := range value {
					if unicode.Is(unicode.Han, r) {
						t.Errorf("non-English application literal in %s", path)
						break
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
