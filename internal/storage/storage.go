// Package storage = raw snapshots on disk. {dir}/{siteID}/{YYYY-MM-DD}/{sha256(url)}.{html|json}.
// same layout as java RawStorageService. returned path is relative, forward slashes.
package storage

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hneyra/crawler-go/internal/urlx"
)

// Raw = snapshot writer. safe for concurrent use (one file per url+day, atomic rename).
type Raw struct {
	Dir string
	Now func() time.Time // nil -> time.Now
}

// SaveHTML writes html snapshot. returns relative path.
func (r Raw) SaveHTML(siteID int64, url, html string) (string, error) {
	return r.save(siteID, url, "html", []byte(html))
}

// SaveJSON writes json snapshot. key = url (or url#intercepted-<xhr url>). returns relative path.
func (r Raw) SaveJSON(siteID int64, key, json string) (string, error) {
	return r.save(siteID, key, "json", []byte(json))
}

func (r Raw) save(siteID int64, key, ext string, body []byte) (string, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	// only digits, a date and hex -> no path traversal from site content.
	rel := path.Join(strconv.FormatInt(siteID, 10), now().UTC().Format("2006-01-02"), urlx.SHA256(key)+"."+ext)
	full := filepath.Join(r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", fmt.Errorf("snapshot dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".snap-*")
	if err != nil {
		return "", fmt.Errorf("snapshot temp: %w", err)
	}
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("write snapshot %s: %w", rel, firstErr(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("rename snapshot %s: %w", rel, err)
	}
	return rel, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
