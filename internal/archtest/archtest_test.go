// Package archtest enforces layer rules (hexagonal). parse imports, no exec.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const mod = "github.com/hneyra/crawler-go/"

// domain pkgs: pure. no transport, no driver, no adapter.
// a missing dir = module not written yet -> skipped.
var domain = []string{"apperr", "actor", "crawl", "discovery", "extraction", "fetch", "urlx", "storage", "sitesconfig", "identity", "jobs"}

// fetch talks http by nature -> may import net/http. still no db, no transport.
var allowHTTP = map[string]bool{"fetch": true}

var forbidden = []string{
	"net/http",
	"database/sql",
	"github.com/jackc/pgx",
	"github.com/getkin",
	"github.com/oapi-codegen",
	"github.com/chromedp",
	mod + "internal/postgres",
	mod + "internal/httpapi",
	mod + "internal/render",
	mod + "internal/platform/db",
	mod + "internal/platform/httpx",
	mod + "cmd",
}

func TestDomainPackagesStayPure(t *testing.T) {
	for _, pkg := range domain {
		dir := filepath.Join("..", pkg)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				for _, bad := range forbidden {
					if bad == "net/http" && allowHTTP[pkg] {
						continue
					}
					if p == bad || strings.HasPrefix(p, bad+"/") {
						t.Errorf("%s imports %s (domain must not depend on adapters/transport)", path, p)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// postgres adapter must not depend on transport.
func TestAdaptersDoNotDependOnTransport(t *testing.T) {
	dir := filepath.Join("..", "postgres")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("postgres adapter not written yet")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, mod+"internal/httpapi") || p == "net/http" {
				t.Errorf("postgres/%s imports %s", e.Name(), p)
			}
		}
	}
}
