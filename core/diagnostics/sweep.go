package diagnostics

import "regexp"

// UnregisteredMarker replaces an identifier the masker never knew about.
//
// It names the failure rather than hiding it. A silent "[redacted]" would keep
// the user safe and let the missing rule live forever; this one is asserted
// against in the tests, so forgetting to register a new field turns the suite
// red instead of turning up in a public issue.
// It names the failure rather than hiding it, so it is only ever used on the
// part of the report that is built out of registered fields. Free text gets
// RedactedMarker instead: see the two functions below.
const UnregisteredMarker = "[redacted: unregistered]"

// RedactedMarker replaces an identifier found in free text — a log line, or a
// comment the user typed.
//
// Free text is not built out of fields, so an identifier in it is not evidence
// that anyone forgot anything. A session ID in a log line belongs to no field
// and no registration can ever cover it; measured on a Store build with two
// profiles, nine such lines carried twenty-seven of them. Marking those as
// unregistered made "the report must contain no unregistered marker" a check
// that could never pass on that machine, and a check that is permanently red
// is a check nobody reads. This marker states what happened and claims nothing
// about why.
const RedactedMarker = "[redacted]"

var (
	// Deliberately loose. A false positive costs one line of a bug report; a
	// false negative costs someone their address in a search index.
	emailShape = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.[\p{L}]{2,}`)

	// notHex is the context a hex run needs on either side to count as a
	// standalone identifier rather than a fragment of something longer.
	//
	// Go's RE2 has no lookaround, so the \b this used to lean on cannot be
	// tightened in place: \b treats "_" as a word character, so it never
	// fires between "_" and a hex digit, and "local_<uuid>.json" — the exact
	// shape every Claude Code session file on disk uses — sailed through
	// unmasked. An explicit non-hex-character class on both sides catches
	// that, the trailing side of the same problem, and any alphanumeric
	// neighbour, while still requiring the match's own capture group be put
	// back afterward: see homeRule and boundedRule in mask.go, which solve
	// the identical "RE2 can't look around" problem the same way.
	notHex = `[^0-9a-fA-F]`

	uuidHyphenated = regexp.MustCompile(`(?i)(^|` + notHex + `)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})($|` + notHex + `)`)

	// uuidBare catches a uuid with its hyphens stripped. Lower likelihood,
	// since this app only ever writes canonically hyphenated uuids, but the
	// sweep's whole premise is catching what nobody registered, and a
	// standalone 32-hex-digit run is not something a report has any other
	// reason to contain. The same notHex context on both sides keeps this
	// from firing inside a longer hex string that merely happens to contain
	// 32 hex digits in a row — a hex-character neighbour on either side
	// means it is part of something bigger, not a 32-digit identifier.
	uuidBare = regexp.MustCompile(`(?i)(^|` + notHex + `)([0-9a-f]{32})($|` + notHex + `)`)
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
//
// Use it on text assembled from registered fields. For a log line or a user's
// own comment, use SweepFreeText.
func Sweep(s string) string { return sweepWith(s, UnregisteredMarker) }

// SweepFreeText redacts the same shapes as Sweep, but says only that something
// was redacted. See RedactedMarker for why the distinction is worth a second
// function rather than a second argument at every call site.
func SweepFreeText(s string) string { return sweepWith(s, RedactedMarker) }

func sweepWith(s, marker string) string {
	s = emailShape.ReplaceAllString(s, marker)
	s = sweepUUID(uuidHyphenated, s, marker)
	s = sweepUUID(uuidBare, s, marker)
	return s
}

// sweepUUID redacts every match of re, putting back the leading and trailing
// context characters the pattern had to consume to establish a non-hex
// boundary — they belong to whatever surrounds the uuid, not to the uuid
// itself. Run twice for the reason mask.go's bounded and home passes are: two
// adjacent matches share the one boundary character between them, and a
// single left-to-right scan only ever awards it to the first.
func sweepUUID(re *regexp.Regexp, s, marker string) string {
	replace := func(match string) string {
		groups := re.FindStringSubmatch(match)
		return groups[1] + marker + groups[3]
	}
	s = re.ReplaceAllStringFunc(s, replace)
	s = re.ReplaceAllStringFunc(s, replace)
	return s
}
