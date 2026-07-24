package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

func TestExtractIdentity(t *testing.T) {
	// react-query-cache-ls shape
	rq := `{"x":{"uuid":"11111111-1111-4111-8111-111111111111","email_address":"first@example.com","full_name":"Example User","display_name":"Example"}}`
	got := extractIdentity(rq)
	if got.Email != "first@example.com" || got.UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("react-query: got %+v", got)
	}
	if got.DisplayName != "Example" || got.FullName != "Example User" {
		t.Fatalf("react-query names: got %+v", got)
	}

	// ajs_user_traits shape (email + account_uuid)
	ajs := `{"traits":{"email":"someone@example.com","account_uuid":"22222222-2222-4222-8222-222222222222"}}`
	got = extractIdentity(ajs)
	if got.Email != "someone@example.com" || got.UUID != "22222222-2222-4222-8222-222222222222" {
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
	payload := `{"uuid":"11111111","email_address":"first@example.com","display_name":"Example","full_name":"Example User"}`
	// 0x01 = Latin-1/UTF-8 encoding tag (see decodeLocalStorageValue).
	if err := db.Put([]byte("_https://claude.ai\x00\x01react-query-cache-ls"), append([]byte{1}, []byte(payload)...), nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	id, err := readLocalStorageIdentity(profile)
	if err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if id.Email != "first@example.com" || id.DisplayName != "Example" || id.UUID != "11111111" {
		t.Fatalf("got %+v", id)
	}
}
