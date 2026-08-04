package diagnostics

import "regexp"

// UnregisteredMarker replaces an identifier the masker never knew about.
//
// It names the failure rather than hiding it. A silent "[redacted]" would keep
// the user safe and let the missing rule live forever; this one is asserted
// against in the tests, so forgetting to register a new field turns the suite
// red instead of turning up in a public issue.
const UnregisteredMarker = "[redacted: unregistered]"

var (
	// Deliberately loose. A false positive costs one line of a bug report; a
	// false negative costs someone their address in a search index.
	emailShape = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.[\p{L}]{2,}`)
	uuidShape  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// Sweep is the backstop, not the mechanism.
//
// Registration only masks what someone thought to register, so a field added
// later leaks by default with nothing to say so. Sweep matches by shape instead:
// anything still looking like an address or a uuid after masking is a value that
// escaped registration.
//
// A swept value loses its identity — two occurrences can no longer be recognised
// as the same thing — which is exactly why it is a last resort and not a
// substitute for registering properly.
func Sweep(s string) string {
	s = emailShape.ReplaceAllString(s, UnregisteredMarker)
	return uuidShape.ReplaceAllString(s, UnregisteredMarker)
}
