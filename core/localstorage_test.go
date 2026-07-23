package core

import "testing"

func TestDecodeLocalStorageValue(t *testing.T) {
	// Latin-1/UTF-8 form: leading byte 1, then raw bytes.
	if got := decodeLocalStorageValue([]byte{1, 'h', 'i'}); got != "hi" {
		t.Errorf("utf8 decode = %q want %q", got, "hi")
	}
	// UTF-16LE form: leading byte 0, then 16-bit little-endian code units.
	utf16le := []byte{0, 'h', 0, 'i', 0}
	if got := decodeLocalStorageValue(utf16le); got != "hi" {
		t.Errorf("utf16 decode = %q want %q", got, "hi")
	}
	if got := decodeLocalStorageValue(nil); got != "" {
		t.Errorf("empty decode = %q want \"\"", got)
	}
}

func TestExtractOrgs(t *testing.T) {
	blob := `{"account":{"organizations":[` +
		`{"name":"Fontrip","rate_limit_tier":"default_raven","billing_type":"stripe_subscription"},` +
		`{"name":"x's Organization","rate_limit_tier":"default_claude_ai","billing_type":"none"}` +
		`]}}`
	orgs := extractOrgs(blob)
	if len(orgs) != 2 {
		t.Fatalf("got %d orgs want 2: %+v", len(orgs), orgs)
	}
	if orgs[0].Name != "Fontrip" || orgs[0].Tier != "default_raven" {
		t.Errorf("org0 = %+v", orgs[0])
	}
	// Non-JSON and org-free JSON return nothing, never panic.
	if got := extractOrgs("buttery"); got != nil {
		t.Errorf("non-json = %+v want nil", got)
	}
	if got := extractOrgs(`{"foo":1}`); got != nil {
		t.Errorf("no-org json = %+v want nil", got)
	}
}
