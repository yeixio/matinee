package api

import (
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/podcast"
	"github.com/yeixio/matinee/internal/store"
)

var updateErrors = flag.Bool("update-errors", false, "rewrite i18n/locales/en/errors.json from the API's error messages")

const errorsCatalog = "../../i18n/locales/en/errors.json"

// Functions whose string results are error messages for writeError.
var messageFuncs = []string{"validate", "validatePassword", "changes", "apply", "checkCommand"}

// Catalog keys ending like these are one form of a plural.
var pluralForm = regexp.MustCompile(`_(zero|one|two|few|many|other)$`)

// apiErrorMessages finds every error message the API can answer with whole:
// literals passed to writeError, writeErrorWith and badRequest, and those
// the validation functions return. Messages from other packages are listed.
func apiErrorMessages(t *testing.T) []string {
	t.Helper()
	msgs := []string{auth.ErrBadCredentials.Error(), store.ErrLastAdmin.Error(), store.ErrPodcastTitleTaken.Error(),
		podcast.ErrBadURL.Error(), podcast.ErrNotFeed.Error(), podcast.ErrNoEpisodes.Error()}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	lit := func(e ast.Expr) {
		if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.STRING {
			if s, err := strconv.Unquote(b.Value); err == nil && s != "" {
				msgs = append(msgs, s)
			}
		}
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok {
					switch id.Name {
					case "writeError", "writeErrorWith":
						if len(n.Args) >= 3 {
							lit(n.Args[2])
						}
					case "badRequest":
						lit(n.Args[0])
					}
				}
			case *ast.FuncDecl:
				if slices.Contains(messageFuncs, n.Name.Name) && n.Body != nil {
					ast.Inspect(n.Body, func(m ast.Node) bool {
						if r, ok := m.(*ast.ReturnStmt); ok {
							for _, e := range r.Results {
								lit(e)
							}
						}
						return true
					})
				}
			}
			return true
		})
	}
	slices.Sort(msgs)
	return slices.Compact(msgs)
}

// TestErrorCatalog checks every whole error message has its English text in
// the catalog under its code, so clients can show it in other languages.
// go test ./internal/api -run TestErrorCatalog -update-errors rewrites it.
func TestErrorCatalog(t *testing.T) {
	want := map[string]string{}
	for _, m := range apiErrorMessages(t) {
		code := errorCode(m)
		if pluralForm.MatchString(code) {
			t.Errorf("%q's code %s ends like a plural form, which the catalog reads as one; reword it", m, code)
		}
		if other, ok := want[code]; ok && other != m {
			t.Errorf("%q and %q have the same code %s", m, other, code)
		}
		want[code] = m
	}
	if len(want) < 50 {
		t.Fatalf("found only %d messages; is the scan still finding writeError?", len(want))
	}
	if *updateErrors {
		b, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(errorsCatalog, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(errorsCatalog)
	if err != nil {
		t.Fatal(err)
	}
	var have map[string]string
	if err := json.Unmarshal(b, &have); err != nil {
		t.Fatal(err)
	}
	for code, m := range want {
		if have[code] != m {
			t.Errorf("errors.json: %s should be %q, is %q (go test ./internal/api -run TestErrorCatalog -update-errors)", code, m, have[code])
		}
	}
	for code := range have {
		if _, ok := want[code]; !ok {
			t.Errorf("errors.json: %s is no longer used (go test ./internal/api -run TestErrorCatalog -update-errors)", code)
		}
	}
}
