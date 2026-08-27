package fileops

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestResolvePath_Matrix(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	outside := filepath.Join(tmpDir, "outside")
	os.MkdirAll(filepath.Join(root, ".ssh"), 0755)
	os.MkdirAll(outside, 0755)

	secretPath := filepath.Join(root, ".ssh", "id_rsa")
	nonSecretPath := filepath.Join(root, "public.txt")
	outsidePath := filepath.Join(outside, "file.txt")
	traversalPath := filepath.Join(root, "..", "outside", "file.txt")
	symlinkPath := filepath.Join(root, "symlink_to_outside")
	
	os.WriteFile(secretPath, []byte("key"), 0600)
	os.WriteFile(nonSecretPath, []byte("data"), 0644)
	os.WriteFile(outsidePath, []byte("data"), 0644)
	os.Symlink(outsidePath, symlinkPath)

	type TestCase struct {
		name          string
		allowSecrets  bool
		unsafe        bool
		confine       bool
		target        string
		expectError   bool
	}

	var cases []TestCase

	for _, allowSecrets := range []bool{false, true} {
		for _, unsafe := range []bool{false, true} {
			for _, confine := range []bool{false, true} {
				// Secret Path
				expectSecretErr := !allowSecrets && !unsafe
				cases = append(cases, TestCase{
					name:         fmt.Sprintf("Secret/allowSecrets=%v/unsafe=%v/confine=%v", allowSecrets, unsafe, confine),
					allowSecrets: allowSecrets,
					unsafe:       unsafe,
					confine:      confine,
					target:       secretPath,
					expectError:  expectSecretErr,
				})

				// Non-secret Path (inside root)
				cases = append(cases, TestCase{
					name:         fmt.Sprintf("NonSecret/allowSecrets=%v/unsafe=%v/confine=%v", allowSecrets, unsafe, confine),
					allowSecrets: allowSecrets,
					unsafe:       unsafe,
					confine:      confine,
					target:       nonSecretPath,
					expectError:  false,
				})

				// Outside Path
				expectOutsideErr := confine && !unsafe
				cases = append(cases, TestCase{
					name:         fmt.Sprintf("Outside/allowSecrets=%v/unsafe=%v/confine=%v", allowSecrets, unsafe, confine),
					allowSecrets: allowSecrets,
					unsafe:       unsafe,
					confine:      confine,
					target:       outsidePath,
					expectError:  expectOutsideErr,
				})

				// Traversal
				cases = append(cases, TestCase{
					name:         fmt.Sprintf("Traversal/allowSecrets=%v/unsafe=%v/confine=%v", allowSecrets, unsafe, confine),
					allowSecrets: allowSecrets,
					unsafe:       unsafe,
					confine:      confine,
					target:       traversalPath,
					expectError:  expectOutsideErr,
				})

				// Symlink escape
				cases = append(cases, TestCase{
					name:         fmt.Sprintf("SymlinkEscape/allowSecrets=%v/unsafe=%v/confine=%v", allowSecrets, unsafe, confine),
					allowSecrets: allowSecrets,
					unsafe:       unsafe,
					confine:      confine,
					target:       symlinkPath,
					expectError:  expectOutsideErr,
				})
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Safety.ConfineToRoots = tc.confine
			cfg.Safety.AllowSecrets = tc.allowSecrets
			cfg.Index.IndexDirs = []string{root}

			// Chdir into root so CWD check doesn't interfere
			origCwd, _ := os.Getwd()
			os.Chdir(root)
			defer os.Chdir(origCwd)

			_, err := ResolvePath(tc.target, tc.unsafe, &cfg)
			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			} else if !tc.expectError && err != nil {
				t.Errorf("Expected success but got err: %v", err)
			}
		})
	}
}

// TestUnsafeOnlyFromCLI enforces that explicitUnsafe arguments in function calls
// to ResolvePath, ReadFile, and Open are never populated from struct fields (e.g. from LLM JSON).
func TestUnsafeOnlyFromCLI(t *testing.T) {
	// Walk the project's ast to find all calls to ResolvePath, ReadFile, Open
	fset := token.NewFileSet()
	repoRoot := filepath.Join("..", "..")

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "vendor") {
			return filepath.SkipDir
		}

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				ident, ok2 := call.Fun.(*ast.Ident)
				if !ok2 {
					return true
				}
				// intra-package call (e.g. inside fileops package)
				name := ident.Name
				if name == "ResolvePath" || name == "ReadFile" || name == "Open" {
					checkUnsafeArg(t, fset, path, name, call.Args)
				}
				return true
			}

			if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "fileops" {
				name := sel.Sel.Name
				if name == "ResolvePath" || name == "ReadFile" || name == "Open" {
					checkUnsafeArg(t, fset, path, name, call.Args)
				}
			}

			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk repo: %v", err)
	}
}

func checkUnsafeArg(t *testing.T, fset *token.FileSet, path, funcName string, args []ast.Expr) {
	if len(args) < 2 {
		return
	}
	arg := args[1] // The explicitUnsafe argument is always the second one

	// We permit boolean literals (true/false)
	if ident, ok := arg.(*ast.Ident); ok {
		if ident.Name == "true" || ident.Name == "false" {
			return
		}
		// Also permit plain variables (which might be CLI flags)
		return
	}
	
	// We do NOT permit passing a struct field (e.g. action.Unsafe)
	if sel, ok := arg.(*ast.SelectorExpr); ok {
		t.Errorf("Security invariant violated in %s: %s argument 2 must not be a struct field, got: %s.%s. It must be a CLI flag or literal.",
			fset.Position(arg.Pos()), funcName, sel.X, sel.Sel.Name)
	}
}
