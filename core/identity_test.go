package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

func TestExtractIdentity(t *testing.T) {
	// react-query-cache-ls shape
	rq := `{"x":{"uuid":"035899b2-b130-40b6-aa9e-93cf208df7b7","email_address":"vincent@fontrip.com","full_name":"Fontrip Vin","display_name":"Vin"}}`
	got := extractIdentity(rq)
	if got.Email != "vincent@fontrip.com" || got.UUID != "035899b2-b130-40b6-aa9e-93cf208df7b7" {
		t.Fatalf("react-query: got %+v", got)
	}
	if got.DisplayName != "Vin" || got.FullName != "Fontrip Vin" {
		t.Fatalf("react-query names: got %+v", got)
	}

	// ajs_user_traits shape (email + account_uuid)
	ajs := `{"traits":{"email":"someone@example.com","account_uuid":"ae543f88-0f24-4ae6-ae21-3033915bca76"}}`
	got = extractIdentity(ajs)
	if got.Email != "someone@example.com" || got.UUID != "ae543f88-0f24-4ae6-ae21-3033915bca76" {
		t.Fatalf("ajs: got %+v", got)
	}

	// non-JSON / no identity → zero value, no panic
	if id := extractIdentity("not json"); id != (AccountIdentity{}) {
		t.Fatalf("expected zero identity, got %+v", id)
	}
}

func TestReadLocalStorageIdentity(t *testing.T) {
	profile := t.TempDir()
	ldb := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(ldb, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := leveldb.OpenFile(ldb, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"uuid":"035899b2","email_address":"vincent@fontrip.com","display_name":"Vin","full_name":"Fontrip Vin"}`
	// 0x01 = Latin-1/UTF-8 encoding tag (see decodeLocalStorageValue).
	if err := db.Put([]byte("_https://claude.ai\x00\x01react-query-cache-ls"), append([]byte{1}, []byte(payload)...), nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	id, err := readLocalStorageIdentity(profile)
	if err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if id.Email != "vincent@fontrip.com" || id.DisplayName != "Vin" || id.UUID != "035899b2" {
		t.Fatalf("got %+v", id)
	}
}
