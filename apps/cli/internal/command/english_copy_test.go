package command

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestCLIProductionCopyContainsNoChineseCharacters(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	cliRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	fset := token.NewFileSet()
	err := filepath.WalkDir(cliRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Errorf("decode string literal in %s: %v", path, err)
				return true
			}
			if containsHan(value) {
				position := fset.Position(literal.Pos())
				t.Errorf("CLI production string at %s contains Chinese characters", position)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan CLI production source: %v", err)
	}

	installer := filepath.Join(cliRoot, "..", "..", "scripts", "install.sh")
	contents, err := os.ReadFile(installer)
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}
	if containsHan(string(contents)) {
		t.Errorf("installer output source %s contains Chinese characters", installer)
	}
}

func containsHan(value string) bool {
	for _, char := range value {
		if unicode.Is(unicode.Han, char) {
			return true
		}
	}
	return false
}
