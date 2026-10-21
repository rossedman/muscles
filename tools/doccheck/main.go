// Command doccheck fails when a function, a method or a package has no doc
// comment. Tests and generated files are skipped.
//
//	go run ./tools/doccheck ./...
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// main checks the directories named by its arguments (dir/... for a tree)
// and exits 1 if anything lacks a doc comment.
func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"./..."}
	}
	n, err := check(os.Stdout, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "doccheck:", err)
		os.Exit(2)
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "doccheck: %d missing doc comments\n", n)
		os.Exit(1)
	}
}

// check reports to w every function without a doc comment and every package
// without a package comment, and returns how many it found.
func check(w io.Writer, args []string) (int, error) {
	fset := token.NewFileSet()
	documented := map[string]bool{} // package directory → has a package comment
	var problems []string
	for _, arg := range args {
		root, recursive := strings.CutSuffix(arg, "/...")
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (!recursive || skipDir(d.Name())) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			if ast.IsGenerated(f) {
				return nil
			}
			dir := filepath.Dir(p)
			documented[dir] = documented[dir] || f.Doc != nil
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Doc == nil {
					problems = append(problems, fmt.Sprintf("%s: %s has no doc comment", fset.Position(fn.Pos()), fn.Name.Name))
				}
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	for dir, ok := range documented {
		if !ok {
			problems = append(problems, dir+": package has no package comment")
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		fmt.Fprintln(w, p)
	}
	return len(problems), nil
}

// skipDir reports whether a directory holds no package code to check.
func skipDir(name string) bool {
	return name == "testdata" || name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}
