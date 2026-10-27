//go:build windows

package platform

import (
	"reflect"
	"testing"
)

func TestClaudeLinkArgs(t *testing.T) {
	const (
		def  = `C:\Users\someone\AppData\Roaming\Claude`
		work = `C:\Users\someone\AppData\Roaming\Claude_Work`
		link = "claude://login/google-auth?code=abc&hop_nonce=n1"
	)
	cases := []struct {
		name, profile, defaultPath string
		want                       []string
	}{
		{"another account", work, def, []string{`--user-data-dir=` + work, link}},
		// The Store build does not keep its default account in %APPDATA%\Claude,
		// so naming that folder would start a second Claude beside the one the
		// user has open instead of reaching it.
		{"the default account", def, def, []string{link}},
		{"the default account, other spelling", `c:\users\someone\appdata\roaming\claude\`, def, []string{link}},
		{"no account named", "", def, []string{link}},
		{"no default known", work, "", []string{`--user-data-dir=` + work, link}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := claudeLinkArgs(c.profile, c.defaultPath, link); !reflect.DeepEqual(got, c.want) {
				t.Errorf("claudeLinkArgs(%q, %q) = %q, want %q", c.profile, c.defaultPath, got, c.want)
			}
		})
	}
}
