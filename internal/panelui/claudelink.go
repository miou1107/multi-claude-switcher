package panelui

// claudelink.go draws what the panel shows about claude:// links (issue #60)
// and about which Claude Desktop the switcher drives (issue #61): the notice on
// the account list, the guide behind its button, and the line at the foot.
//
// Names that Windows shows in its own window are drawn with translate="no":
// the guide has to match what is on screen there, and i18n.js would otherwise
// turn "Multi-Claude Switcher" into the panel's translated name for itself.

import (
	"html"

	"github.com/miou1107/multi-claude-switcher/platform"
)

// The lines a host puts on screen about claude:// links. Here rather than in
// each host so both say the same thing and the translation check reads them.
const (
	// LinksChosenStatus is the list's status once the user picked the switcher.
	LinksChosenStatus = "Sign-in links now go through the switcher."
	// LinkGuideNoWindow is the guide's status when Windows could not be made to
	// ask, which leaves its Settings as the way.
	LinkGuideNoWindow = "Windows did not show its window. In Windows Settings, open Apps, Default apps, Choose default apps by protocol, and pick Multi-Claude Switcher for CLAUDE."
)

// ClaudeAppVM names the Claude Desktop the switcher drives.
type ClaudeAppVM struct {
	Version string // "3.6556.2", or "" when it could not be read
	Edition string // one of platform's Edition constants, or "" when unknown
}

// editionLabel is how the foot of the list names an edition.
func editionLabel(edition string) string {
	switch edition {
	case platform.EditionStore:
		return "Microsoft Store"
	case platform.EditionClaudeCom:
		return "installed from claude.com"
	case platform.EditionMac:
		return "Mac"
	}
	return ""
}

// aboutLine is the foot of the account list: the switcher's version, which
// opens the changelog, then the Claude it drives. A bug report starts with
// both, and before this the second one could only be found in the report.
func aboutLine(version string, claude ClaudeAppVM) string {
	esc := html.EscapeString
	s := `<div class="about"><button class="aboutlink" title="What's new" onclick="send('openChangelog','')"><span>Switcher</span> v` + esc(version) + `</button>`
	label := editionLabel(claude.Edition)
	if claude.Version == "" && label == "" {
		return s + `</div>`
	}
	s += `<span class="sep">·</span><span class="aboutclaude"><span translate="no">Claude`
	if claude.Version != "" {
		s += ` ` + esc(claude.Version)
	}
	s += `</span>`
	if label != "" {
		s += ` · <span>` + label + `</span>`
	}
	return s + `</span></div>`
}

// linkNotice is the notice above the accounts while sign-in links land on the
// default account. It has no "Not now": signing in to another account is broken
// until this is done, so it stays until the switcher is chosen, and it says so
// when the user picked another app in Windows' window.
func linkNotice(links platform.ClaudeLinkStatus) string {
	if !links.NeedsSwitcher() {
		return ""
	}
	missed := ""
	if links == platform.ClaudeLinksOtherAppPicked {
		missed = `<div class="lnmiss">Windows still opens claude:// links with another app, not Multi-Claude Switcher.</div>`
	}
	return `<div class="linknote">
  <div class="lntitle">Sign-in links need one more step</div>
  <div>Claude from the Microsoft Store opens every sign-in link on its default account, so signing in to another account can get stuck. Choose Multi-Claude Switcher for claude:// links once, and the switcher sends each link to the account you are on. This stays here until it is set up.</div>` + missed + `
  <button class="btn btn-primary" onclick="send('showLinkGuide','')">Choose Multi-Claude Switcher</button>
</div>`
}

// switcherLogo is the switcher's own icon (cmd/mcs-tray/assets), which is what
// Windows shows beside its name in the window the guide describes.
const switcherLogo = `<svg viewBox="0 0 1024 1024" aria-hidden="true"><defs><linearGradient id="mcsg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#e68a6c"/><stop offset="1" stop-color="#c3603c"/></linearGradient></defs>` +
	`<rect x="62" y="62" width="900" height="900" rx="220" fill="url(#mcsg)"/>` +
	`<circle cx="379" cy="511" r="215" fill="#fff"/><circle cx="379" cy="511" r="87" fill="#3b1f16"/><circle cx="414" cy="476" r="26" fill="#fff"/>` +
	`<circle cx="757" cy="542" r="123" fill="#fff"/><circle cx="757" cy="542" r="51" fill="#3b1f16"/><circle cx="778" cy="517" r="15" fill="#fff"/></svg>`

// claudeLogo stands for Claude's icon in the same window.
const claudeLogo = `<svg viewBox="0 0 24 24" aria-hidden="true"><rect width="24" height="24" rx="5" fill="#d97757"/>` +
	`<g stroke="#fff" stroke-width="1.9" stroke-linecap="round"><path d="M12 5v14M5 12h14M7.1 7.1l9.8 9.8M16.9 7.1l-9.8 9.8"/></g></svg>`

// RenderLinkGuide is the screen behind the notice's button. It shows what
// Windows' "How do you want to open this?" window looks like and where to click,
// before the button that makes Windows show it. status reports a try that did
// not open the window.
func RenderLinkGuide(status string) string {
	statusBanner := ""
	if status != "" {
		statusBanner = `<div class="hintw" style="margin:0 0 10px">` + html.EscapeString(status) + `</div>`
	}
	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Choose the switcher</h1><p>One time, so sign-in links reach the right account</p></div>
</div>` + statusBanner + `
<div class="hint" style="margin-top:0">Windows will ask which app opens claude:// links. In that window:</div>
<div class="gsteps">
  <div><b>①</b><span>Click Multi-Claude Switcher (recommended)</span></div>
  <div><b>②</b><span>Leave Always use this app ticked</span></div>
  <div><b>③</b><span>Click OK</span></div>
</div>
<div class="gpick" aria-hidden="true">
  <div class="gq">How do you want to open this?</div>
  <div class="gapp">` + claudeLogo + `<span translate="no">Claude</span></div>
  <div class="gapp gon">` + switcherLogo + `<span><span translate="no">Multi-Claude Switcher</span> <span>(recommended)</span></span><span class="gmark">①</span></div>
  <div class="gbox"><span class="gtick">✓</span><span>Always use this app</span><span class="gmark">②</span></div>
  <div class="gok"><span class="gokbtn">OK</span><span class="gmark">③</span></div>
</div>
<div class="footer"><button class="btn btn-primary" onclick="send('askWindowsForLinkApp','')">Open the Windows window</button></div>`
	return shell(body)
}
