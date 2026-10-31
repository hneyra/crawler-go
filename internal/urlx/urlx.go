// Package urlx = url normalize/resolve + content hash. same rules as java crawler UrlUtils/HashUtils.
package urlx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// ErrInvalid = unparsable or non-absolute url.
var ErrInvalid = errors.New("invalid url")

// Normalize: trim, drop fragment only. query + order kept, no case change, no slash change.
// dedupe key for discovered urls -> must stay stable.
func Normalize(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", ErrInvalid
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}

// Resolve: absolute http(s) kept as is, else resolved against base. "" on failure.
func Resolve(ref, base string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

// SHA256 = lowercase hex of utf-8 bytes. 64 chars.
func SHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
