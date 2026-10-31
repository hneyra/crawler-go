package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hneyra/crawler-go/internal/apperr"
)

func TestWrapKeepsKindHidesCause(t *testing.T) {
	cause := errors.New("sql: no rows")
	err := fmt.Errorf("load site 7: %w", apperr.NotFound("site_not_found", "Sitio no encontrado").Wrap(cause))
	if !apperr.IsKind(err, apperr.KindNotFound) || !errors.Is(err, cause) {
		t.Fatalf("kind/cause lost: %v", err)
	}
	ae, _ := apperr.As(err)
	if ae.Message != "Sitio no encontrado" || ae.Code != "site_not_found" {
		t.Fatalf("got %+v", ae)
	}
}

func TestFieldsErr(t *testing.T) {
	var f apperr.Fields
	if f.Err("x", "y") != nil {
		t.Fatal("empty fields must be nil")
	}
	f.Add("url", "invalid_url", "URL inválida")
	err := f.Err("invalid_site", "Sitio inválido")
	ae, ok := apperr.As(err)
	if !ok || ae.Kind != apperr.KindInvalid || len(ae.Fields) != 1 {
		t.Fatalf("got %v", err)
	}
}
