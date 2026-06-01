package listquery_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

func TestParsePageDefaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	p, err := listquery.ParsePage(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Page != 1 || p.PerPage != 20 {
		t.Fatalf("got page=%d per_page=%d", p.Page, p.PerPage)
	}
}

func TestParsePageCustom(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=2&per_page=10", nil)
	p, err := listquery.ParsePage(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Page != 2 || p.PerPage != 10 || p.Offset() != 10 {
		t.Fatalf("page=%d per_page=%d offset=%d", p.Page, p.PerPage, p.Offset())
	}
}

func TestParsePageRejectsHighPerPage(t *testing.T) {
	r := httptest.NewRequest("GET", "/?per_page=500", nil)
	if _, err := listquery.ParsePage(r); err == nil {
		t.Fatal("expected error")
	}
}

func TestSettlementCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	created := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	token := listquery.EncodeSettlementCursor(created, id)
	decoded, err := listquery.DecodeSettlementCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(created) || decoded.ID != id {
		t.Fatalf("cursor mismatch: %+v", decoded)
	}
}

func TestParseVaultListSortWhitelist(t *testing.T) {
	r := httptest.NewRequest("GET", "/?sort=not_a_column", nil)
	if _, err := listquery.ParseVaultList(r); err == nil {
		t.Fatal("expected invalid sort error")
	}
}
