package storage_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/storage"
)

// port of java RawStorageServiceTest.
func TestRawPathsAndContent(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 3, 9, 23, 30, 0, 0, time.FixedZone("PET", -5*3600)) // utc next day
	r := storage.Raw{Dir: dir, Now: func() time.Time { return day }}
	p1, err := r.SaveHTML(7, "https://shop.pe/p/1", "<html>1</html>")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^7/2026-03-10/[0-9a-f]{64}\.html$`).MatchString(p1) {
		t.Fatalf("path %q", p1)
	}
	b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p1)))
	if string(b) != "<html>1</html>" {
		t.Fatalf("content %q", b)
	}
	again, _ := r.SaveHTML(7, "https://shop.pe/p/1", "<html>v2</html>")
	b, _ = os.ReadFile(filepath.Join(dir, filepath.FromSlash(again)))
	if again != p1 || string(b) != "<html>v2</html>" {
		t.Fatalf("overwrite: %q %q", again, b)
	}
	other, _ := r.SaveHTML(7, "https://shop.pe/p/2", "x")
	otherSite, _ := r.SaveHTML(8, "https://shop.pe/p/1", "x")
	if other == p1 || otherSite == p1 {
		t.Fatal("paths must differ by url and site")
	}
	j, err := r.SaveJSON(7, "https://shop.pe/p/1#intercepted-https://api.shop.pe/x", `{"a":1}`)
	if err != nil || filepath.Ext(j) != ".json" {
		t.Fatalf("json: %q %v", j, err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "7", "2026-03-10"))
	for _, e := range entries {
		if e.Name()[0] == '.' {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestRawUnwritableDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	if _, err := (storage.Raw{Dir: f}).SaveHTML(1, "u", "x"); err == nil {
		t.Fatal("want error")
	}
}
