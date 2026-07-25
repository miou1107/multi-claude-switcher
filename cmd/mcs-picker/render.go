package main

import (
	"fmt"
	"html"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
)

// computePreselect returns the set of HomeFolders to pre-check. managed == nil
// (first run) → every complete account; any non-nil slice (incl. empty) → only
// the listed folders. Ghosts are never included.
func computePreselect(accounts []core.ScannedAccount, managed []string) map[string]bool {
	firstRun := managed == nil
	set := map[string]bool{}
	for _, m := range managed {
		set[m] = true
	}
	pre := map[string]bool{}
	for _, a := range accounts {
		if !a.Complete {
			continue
		}
		if firstRun || set[a.HomeFolder] {
			pre[a.HomeFolder] = true
		}
	}
	return pre
}

// teamPill returns the badge text and CSS class for an account's Team column.
func teamPill(a core.ScannedAccount) (string, string) {
	if !a.Complete {
		return "", ""
	}
	switch a.Account {
	case core.AccountTeam:
		return "🏢 Team", "pill-team"
	case core.AccountPersonal:
		return "Personal", "pill-personal"
	default:
		return "Unknown", "pill-unknown"
	}
}

func short(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// renderPicker builds the full, self-contained picker page: a soft-gradient
// panel with one card per account (checkable for complete accounts, greyed for
// ghosts), a Team badge, and Cancel / Confirm actions. It mirrors the ClaudeBar
// menu-bar aesthetic.
func renderPicker(accounts []core.ScannedAccount, preselected map[string]bool) string {
	esc := html.EscapeString
	var cards strings.Builder
	completeCount := 0

	for _, a := range accounts {
		team, teamCls := teamPill(a)
		date := "—"
		if !a.LastUpdated.IsZero() {
			date = a.LastUpdated.Format("2006-01-02")
		}

		if !a.Complete {
			// Ghost card: greyed, not selectable.
			cards.WriteString(fmt.Sprintf(`
      <div class="card ghost">
        <div class="check-slot"></div>
        <div class="body">
          <div class="row1"><span class="email muted">Unrecognized account</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-bad">%s</div>
        </div>
      </div>`, esc(short(a.UUID)), a.Convos, esc(date), esc(a.Note)))
			continue
		}

		completeCount++
		name := a.Email
		if name == "" {
			name = a.HomeFolder
		}
		selCls := ""
		checked := ""
		if preselected[a.HomeFolder] {
			selCls = " selected"
			checked = " checked"
		}
		pill := ""
		if team != "" {
			pill = fmt.Sprintf(`<span class="pill %s">%s</span>`, teamCls, esc(team))
		}
		cards.WriteString(fmt.Sprintf(`
      <label class="card selectable%s" data-folder="%s">
        <div class="check-slot"><input type="checkbox" class="chk"%s></div>
        <div class="body">
          <div class="row1"><span class="email">%s</span>%s</div>
          <div class="folder">%s</div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>Updated %s</div>
        </div>
      </label>`, selCls, esc(a.HomeFolder), checked, esc(name), pill, esc(a.HomeFolder), esc(short(a.UUID)), a.Convos, esc(date)))
	}

	sub := "Choose the accounts Multi-Claude Switcher should manage."
	if completeCount == 0 {
		sub = "No switchable accounts were found — only orphaned data."
	}

	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Multi-Claude Switcher</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
html{color-scheme:light}
body{
  font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif;
  color:#241f38;
  background:linear-gradient(160deg,#efe9fb 0%,#f6eaf2 55%,#f9edf1 100%);
  padding:20px;
  -webkit-font-smoothing:antialiased;
}
.panel{max-width:600px;margin:0 auto}
.header{display:flex;align-items:center;gap:14px;margin:4px 4px 18px}
.avatar{
  width:52px;height:52px;border-radius:16px;flex:none;
  background:linear-gradient(140deg,#8a74f0,#b96cee 55%,#e0607a);
  display:flex;align-items:center;justify-content:center;
  box-shadow:0 6px 16px rgba(124,108,240,.35);
}
.avatar svg{width:28px;height:28px}
.htext h1{font-size:20px;font-weight:800;letter-spacing:-.01em}
.htext p{font-size:13px;color:#6b6580;margin-top:2px}
.cards{display:flex;flex-direction:column;gap:12px}
.card{
  display:flex;align-items:center;gap:14px;
  background:#fff;border-radius:18px;padding:16px 18px;
  box-shadow:0 4px 14px rgba(60,40,90,.07);
  border:2px solid transparent;transition:.15s ease;
}
.card.selectable{cursor:pointer}
.card.selectable:hover{box-shadow:0 6px 18px rgba(60,40,90,.12)}
.card.selected{border-color:#7c6cf0;box-shadow:0 6px 20px rgba(124,108,240,.22)}
.card.ghost{opacity:.6}
.check-slot{width:26px;flex:none;display:flex;align-items:center;justify-content:center}
.chk{
  appearance:none;-webkit-appearance:none;width:22px;height:22px;border-radius:7px;
  border:2px solid #cdc8e0;background:#fff;cursor:pointer;position:relative;transition:.15s;
}
.selected .chk{background:#7c6cf0;border-color:#7c6cf0}
.chk:checked::after{content:"";position:absolute;left:6px;top:2px;width:6px;height:11px;border:solid #fff;border-width:0 2.5px 2.5px 0;transform:rotate(45deg)}
.body{flex:1;min-width:0}
.row1{display:flex;align-items:center;gap:8px}
.email{font-size:16px;font-weight:700;letter-spacing:-.01em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.email.muted{color:#8b8598;font-weight:600}
.folder{font-size:12px;color:#8b8598;margin-top:1px}
.meta{font-size:12px;color:#8b8598;margin-top:6px;display:flex;align-items:center;gap:6px;flex-wrap:wrap}
.chip{font-family:ui-monospace,SFMono-Regular,monospace;font-size:11px;background:#f1eef9;color:#6b6580;padding:2px 7px;border-radius:6px}
.dot{opacity:.5}
.pill{font-size:11px;font-weight:700;padding:3px 9px;border-radius:999px;white-space:nowrap}
.pill-team{background:#d6f5e3;color:#1a8a4f}
.pill-personal{background:#eceaf3;color:#6b6580}
.pill-unknown{background:#f3ecd6;color:#8a6d1a}
.note-bad{margin-top:6px;display:inline-block;font-size:11px;font-weight:700;background:#fde4e4;color:#c0392b;padding:3px 9px;border-radius:999px}
.footer{display:flex;justify-content:flex-end;gap:12px;margin:22px 4px 6px}
button{font:inherit;font-weight:700;font-size:14px;border:none;cursor:pointer;border-radius:14px;padding:12px 22px;transition:.15s}
.btn-cancel{background:#fff;color:#514b66;box-shadow:0 3px 10px rgba(60,40,90,.08)}
.btn-cancel:hover{background:#f6f4fb}
.btn-confirm{background:linear-gradient(135deg,#7c6cf0,#9b6bff);color:#fff;box-shadow:0 6px 16px rgba(124,108,240,.4)}
.btn-confirm:hover{filter:brightness(1.05)}
.btn-confirm:disabled{opacity:.45;cursor:not-allowed;box-shadow:none}
</style></head><body>
<div class="panel">
  <div class="header">
    <div class="avatar"><svg viewBox="0 0 24 24" fill="none"><circle cx="8" cy="12" r="3.2" fill="#fff"/><circle cx="16" cy="12" r="3.2" fill="#fff"/><circle cx="8" cy="12" r="1.3" fill="#7c6cf0"/><circle cx="16" cy="12" r="1.3" fill="#7c6cf0"/></svg></div>
    <div class="htext"><h1>Multi-Claude Switcher</h1><p>` + sub + `</p></div>
  </div>
  <div class="cards">` + cards.String() + `</div>
  <div class="footer">
    <button class="btn-cancel" onclick="mcsCancel()">Cancel</button>
    <button class="btn-confirm" id="confirm" onclick="doConfirm()">Confirm</button>
  </div>
</div>
<script>
  document.querySelectorAll('.card.selectable').forEach(function(card){
    card.addEventListener('click',function(e){
      e.preventDefault();
      card.classList.toggle('selected');
      card.querySelector('.chk').checked=card.classList.contains('selected');
    });
  });
  function doConfirm(){
    var picked=[];
    document.querySelectorAll('.card.selectable.selected').forEach(function(c){picked.push(c.dataset.folder);});
    window.mcsSubmit(picked);
  }
</script>
</body></html>`
}
