package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// AccountIdentity is the human-readable identity of a profile's live-login
// account, read from its cached Local Storage payloads. Fields are best-effort:
// any may be empty if the store lacks them.
type AccountIdentity struct {
	UUID        string
	Email       string
	DisplayName string
	FullName    string
}

// extractIdentity walks any nested JSON and fills an AccountIdentity from the two
// known account payloads: `ajs_user_traits` ({email, account_uuid}) and
// `react-query-cache-ls` ({uuid, email_address, full_name, display_name}). Later
// non-empty values win. Returns a zero value for non-JSON or identity-free input
// (never panics).
func extractIdentity(decoded string) AccountIdentity {
	var root interface{}
	if json.Unmarshal([]byte(decoded), &root) != nil {
		i := strings.IndexAny(decoded, "{[")
		if i < 0 {
			return AccountIdentity{}
		}
		if json.Unmarshal([]byte(decoded[i:]), &root) != nil {
			return AccountIdentity{}
		}
	}
	var id AccountIdentity
	set := func(dst *string, v interface{}) {
		if s, ok := v.(string); ok && s != "" {
			*dst = s
		}
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			set(&id.Email, t["email"])
			set(&id.Email, t["email_address"])
			set(&id.UUID, t["account_uuid"])
			set(&id.UUID, t["uuid"])
			set(&id.DisplayName, t["display_name"])
			set(&id.FullName, t["full_name"])
			for _, vv := range t {
				walk(vv)
			}
		case []interface{}:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(root)
	return id
}

// readLocalStorageIdentity copies the profile's Local Storage LevelDB to a temp
// dir (the live store is locked while Claude runs), opens it, and merges identity
// from every value that looks like an account payload. Best-effort: returns the
// merged identity and any fatal open/copy error.
func readLocalStorageIdentity(profilePath string) (AccountIdentity, error) {
	src := filepath.Join(profilePath, "Local Storage", "leveldb")
	if _, err := os.Stat(src); err != nil {
		return AccountIdentity{}, fmt.Errorf("local storage not found for %s: %w", profilePath, err)
	}
	tmp, err := os.MkdirTemp("", "mcs-id-*")
	if err != nil {
		return AccountIdentity{}, err
	}
	defer os.RemoveAll(tmp)

	dst := filepath.Join(tmp, "leveldb")
	if err := copyDir(src, dst); err != nil {
		return AccountIdentity{}, fmt.Errorf("copy local storage: %w", err)
	}
	db, err := leveldb.OpenFile(dst, &opt.Options{ReadOnly: true})
	if err != nil {
		if db, err = leveldb.OpenFile(dst, nil); err != nil {
			return AccountIdentity{}, fmt.Errorf("open leveldb: %w", err)
		}
	}
	defer db.Close()

	var id AccountIdentity
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		s := decodeLocalStorageValue(it.Value())
		if !strings.Contains(s, "@") {
			continue // identity payloads always contain an email
		}
		got := extractIdentity(s)
		if got.Email != "" {
			id.Email = got.Email
		}
		if got.UUID != "" {
			id.UUID = got.UUID
		}
		if got.DisplayName != "" {
			id.DisplayName = got.DisplayName
		}
		if got.FullName != "" {
			id.FullName = got.FullName
		}
	}
	if err := it.Error(); err != nil {
		return id, err
	}
	return id, nil
}
