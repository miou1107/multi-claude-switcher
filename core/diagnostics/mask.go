// Package diagnostics turns what MCS knows about a machine into a report a user
// can publish. Its job is not gathering — each host does that — but masking and
// formatting, which is the part worth testing and the part that must not depend
// on which platform it runs on.
package diagnostics

import (
	"fmt"
	"sort"
	"strings"
)

// Masker replaces identifying values with stable pseudonyms.
//
// Pseudonyms rather than asterisks, because "vin***@fontrip.com" still gives up
// a name and an employer, and two occurrences of it cannot be told apart. A
// report is made of relationships — this account's conversations turned up in
// that account's folder — and a pseudonym keeps every relationship while giving
// up none of the values.
type Masker struct {
	// byValue maps a raw value to its pseudonym. One table, keyed by value and
	// never by role: a UUID that arrives as both an account and an organization
	// must not answer to two names depending on which was registered first.
	byValue  map[string]string
	accounts int
	orgs     int
}

func NewMasker() *Masker {
	return &Masker{byValue: map[string]string{}}
}

// RegisterAccount ties a UUID and an email to one pseudonym, so a log line that
// only ever mentions the UUID still reads as the same account as the summary
// line that mentions the address. Either may be empty; a profile that is not
// signed in has both.
func (m *Masker) RegisterAccount(uuid, email string) {
	name := ""
	for _, v := range []string{uuid, email} {
		if v == "" {
			continue
		}
		if existing, ok := m.byValue[v]; ok {
			name = existing
			break
		}
	}
	if name == "" {
		if uuid == "" && email == "" {
			return
		}
		m.accounts++
		name = fmt.Sprintf("account-%d", m.accounts)
	}
	m.put(uuid, name)
	m.put(email, name)
}

// RegisterOrg gives an organization UUID a letter. Letters rather than numbers
// so an org is never mistaken for an account at a glance.
func (m *Masker) RegisterOrg(uuid string) {
	if uuid == "" {
		return
	}
	if _, ok := m.byValue[uuid]; ok {
		return
	}
	m.orgs++
	m.put(uuid, "org-"+orgLetter(m.orgs))
}

// RegisterWord registers a value with a caller-chosen replacement, for the
// values that are not identifiers in their own right but give a person away all
// the same: the OS user name, the host name.
func (m *Masker) RegisterWord(value, replacement string) {
	if value == "" {
		return
	}
	m.put(value, replacement)
}

func (m *Masker) put(value, name string) {
	if value == "" {
		return
	}
	if _, ok := m.byValue[value]; ok {
		return
	}
	m.byValue[value] = name
}

// orgLetter numbers organizations A, B, … Z, AA, AB. Sequential rather than
// hashed so two reports from one machine line up.
func orgLetter(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}

// Apply replaces every registered value in s.
//
// Longest first: an email and a UUID can share a prefix with something else
// registered, and replacing the shorter one first would leave a fragment of the
// longer one behind.
func (m *Masker) Apply(s string) string {
	if s == "" || len(m.byValue) == 0 {
		return s
	}
	for _, v := range m.sortedValues() {
		s = strings.ReplaceAll(s, v, m.byValue[v])
	}
	return s
}

func (m *Masker) sortedValues() []string {
	out := make([]string, 0, len(m.byValue))
	for v := range m.byValue {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}
