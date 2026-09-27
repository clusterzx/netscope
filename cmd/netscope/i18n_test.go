package main

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"strconv"
	"testing"

	"netscope/internal/i18n"
)

// TestCLITexts: every text the CLI prints through tr() or returns as an error has an
// English translation (the catalog tests in internal/i18n cannot see package main).
func TestCLITexts(t *testing.T) {
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				name = x.Name + "." + fn.Sel.Name
			}
		}
		if name != "tr" && name != "fmt.Errorf" && name != "errors.New" {
			return true
		}
		switch a := call.Args[0].(type) {
		case *ast.BasicLit:
			s, err := strconv.Unquote(a.Value)
			if err == nil && !i18n.Has(s) {
				t.Errorf("%s: no English translation for %q", fset.Position(a.Pos()), s)
			}
			checked++
		case *ast.Ident:
			if a.Name == "usageText" && !i18n.Has(usageText) {
				t.Error("usage text has no English translation")
			}
			checked++
		}
		return true
	})
	if checked < 10 {
		t.Fatalf("only %d texts found – did the CLI stop using tr()?", checked)
	}
	if i18n.T(i18n.EN, usageText) == usageText {
		t.Error("usage text not translated")
	}
}
