package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/hneyra/crawler-go/internal/apperr"
)

// argon2id params. RFC 9106 memory-constrained profile.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonSaltLen = 16
	argonKeyLen  = 32

	PasswordMin = 12
	PasswordMax = 256
)

var b64 = base64.RawStdEncoding

// HashPassword -> PHC string. $argon2id$v=19$m=..,t=..,p=..$salt$key
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword: constant time compare. garbage hash -> false.
func VerifyPassword(pw, phc string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	// cap params. hostile hash must not burn cpu/mem.
	if m == 0 || m > 256*1024 || t == 0 || t > 10 || p == 0 || p > 16 {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) != argonKeyLen {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, argonKeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword: length in runes, cap in bytes. email as password refused.
func ValidatePassword(pw, email string) error {
	var f apperr.Fields
	switch {
	case utf8.RuneCountInString(pw) < PasswordMin:
		f.Add("password", "too_short", fmt.Sprintf("Mínimo %d caracteres", PasswordMin))
	case len(pw) > PasswordMax:
		f.Add("password", "too_long", fmt.Sprintf("Máximo %d caracteres", PasswordMax))
	case strings.EqualFold(strings.TrimSpace(pw), strings.TrimSpace(email)):
		f.Add("password", "equals_email", "La contraseña no puede ser tu email")
	}
	return f.Err("invalid_password", "Contraseña no válida")
}
