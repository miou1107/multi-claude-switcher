//go:build darwin

package main

import (
	"fmt"
	"html"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
)

// profileVM is one row in the account-list view.
type profileVM struct {
	Folder  string
	Name    string
	Plan    string // subscription label: "Team" | "Max 20×" | "Pro" | "Free" | …
	Current bool
}

// planPill renders the subscription badge for an account.
func planPill(plan string) string {
	if plan == "" {
		return ""
	}
	switch plan {
	case "Team":
		return `<span class="pill pill-team">🏢 Team</span>`
	case "Free", "API":
		return `<span class="pill pill-personal">` + html.EscapeString(plan) + `</span>`
	default: // Max 20×, Max 5×, Max, Pro
		return `<span class="pill pill-plan">` + html.EscapeString(plan) + `</span>`
	}
}

// shell wraps body content in the shared styled page. Every view lives in the
// same popover webview — there are no separate windows.
func shell(body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Multi-Claude Switcher</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html{color-scheme:light}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif;color:#241f38;
  background:linear-gradient(160deg,#efe9fb 0%,#f6eaf2 55%,#f9edf1 100%);padding:16px;-webkit-font-smoothing:antialiased;width:400px;overflow-x:hidden}
.header{display:flex;align-items:center;gap:11px;margin:2px 2px 14px}
.avatar{width:40px;height:40px;border-radius:12px;flex:none;background:linear-gradient(140deg,#8a74f0,#b96cee 55%,#e0607a);
  display:flex;align-items:center;justify-content:center;box-shadow:0 5px 13px rgba(124,108,240,.32)}
.avatar svg{width:22px;height:22px}
.back{width:40px;height:40px;border-radius:12px;flex:none;background:#fff;border:none;cursor:pointer;font-size:18px;color:#7c6cf0;
  display:flex;align-items:center;justify-content:center;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.back:hover{background:#f6f4fb}
.htext h1{font-size:16px;font-weight:800;letter-spacing:-.01em}
.htext p{font-size:12px;color:#6b6580;margin-top:1px}
.cards{display:flex;flex-direction:column;gap:9px}
.card{display:flex;align-items:center;gap:12px;background:#fff;border-radius:14px;padding:12px 14px;
  box-shadow:0 3px 10px rgba(60,40,90,.06);border:2px solid transparent;transition:.13s}
.card.selectable{cursor:pointer}
.card.selectable:hover{box-shadow:0 5px 15px rgba(60,40,90,.13);border-color:#e0dcf3}
.card.selected{border-color:#7c6cf0;background:#faf9ff}
.card.current{border-color:#b7f0cd;background:#fbfffd}
.card.ghost{opacity:.55}
.chev{width:24px;height:24px;flex:none;border-radius:8px;background:#f1eef9;color:#7c6cf0;font-size:14px;display:flex;align-items:center;justify-content:center}
.dotcur{width:9px;height:9px;flex:none;border-radius:50%;background:#1a8a4f;margin:0 7px 0 3px;box-shadow:0 0 0 3px #d6f5e3}
.chk{appearance:none;-webkit-appearance:none;width:21px;height:21px;flex:none;border-radius:6px;border:2px solid #cdc8e0;background:#fff;position:relative}
.selected .chk{background:#7c6cf0;border-color:#7c6cf0}
.chk:checked::after{content:"";position:absolute;left:6px;top:2px;width:6px;height:11px;border:solid #fff;border-width:0 2.5px 2.5px 0;transform:rotate(45deg)}
.body{flex:1;min-width:0}
.row1{display:flex;align-items:center;gap:7px}
.name{font-size:14px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sub{font-size:11.5px;color:#8b8598;margin-top:1px}
.meta{font-size:11px;color:#8b8598;margin-top:5px;display:flex;align-items:center;gap:5px;flex-wrap:wrap}
.chip{font-family:ui-monospace,SFMono-Regular,monospace;font-size:10.5px;background:#f1eef9;color:#6b6580;padding:2px 6px;border-radius:5px}
.dot{opacity:.5}
.pill{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;white-space:nowrap}
.pill-team{background:#d6f5e3;color:#1a8a4f}
.pill-personal{background:#eceaf3;color:#6b6580}
.pill-plan{background:#ece8fb;color:#6a4fd0}
.note-bad{margin-top:5px;display:inline-block;font-size:10.5px;font-weight:700;background:#fde4e4;color:#c0392b;padding:2px 8px;border-radius:999px}
.empty{color:#8b8598;font-size:13px;text-align:center;padding:18px 8px}
.footer{display:flex;gap:9px;margin-top:14px}
.btn{flex:1;font:inherit;font-weight:700;font-size:13px;border:none;cursor:pointer;border-radius:11px;padding:10px;transition:.13s}
.btn-light{background:#fff;color:#514b66;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.btn-light:hover{background:#f6f4fb}
.btn-primary{background:linear-gradient(135deg,#7c6cf0,#9b6bff);color:#fff;box-shadow:0 4px 12px rgba(124,108,240,.4)}
.btn-primary:hover{filter:brightness(1.05)}
.btn-quit{flex:none;padding:10px 15px;background:#fff;color:#b0455f;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.btn-quit:hover{background:#fdf2f5}
.gear{flex:none;padding:10px 14px;background:#fff;color:#514b66;box-shadow:0 3px 9px rgba(60,40,90,.08);font-size:15px}
.gear:hover{background:#f6f4fb}
.slist{display:flex;flex-direction:column;gap:9px}
.srow{display:flex;align-items:center;gap:12px;background:#fff;border-radius:14px;padding:13px 15px;box-shadow:0 3px 10px rgba(60,40,90,.06)}
.slabel{flex:1;min-width:0}
.slabel .t{font-size:14px;font-weight:600}
.slabel .s{font-size:11.5px;color:#8b8598;margin-top:1px}
.sbtn{width:100%;text-align:left;background:#fff;border:none;border-radius:14px;padding:13px 15px;font:inherit;font-size:14px;font-weight:600;cursor:pointer;box-shadow:0 3px 10px rgba(60,40,90,.06);color:#241f38}
.sbtn:hover{background:#faf9ff}
.sbtn.danger{color:#b0455f}
.sbtn:disabled{opacity:.5;cursor:default;color:#8b8598}
.sbtn:disabled:hover{background:#fff}
.toggle{width:44px;height:26px;border-radius:999px;background:#d5d0e6;position:relative;flex:none;transition:.15s;cursor:pointer}
.toggle.on{background:#7c6cf0}
.toggle::after{content:"";position:absolute;top:3px;left:3px;width:20px;height:20px;border-radius:50%;background:#fff;transition:.15s;box-shadow:0 1px 3px rgba(0,0,0,.2)}
.toggle.on::after{left:21px}
.about{text-align:center;font-size:11.5px;color:#8b8598;margin-top:14px}
.status{background:#e3f3e8;color:#1a7a3d;font-size:12.5px;font-weight:600;padding:9px 13px;border-radius:11px;margin-bottom:11px;text-align:center}
.edit{width:30px;height:30px;flex:none;border:none;border-radius:8px;background:transparent;color:#8b8598;cursor:pointer;font-size:14px;opacity:.55}
.edit:hover{background:#f1eef9;opacity:1;color:#7c6cf0}
.rninput{width:100%;font:inherit;font-size:15px;padding:13px 15px;border:2px solid #e0dcf3;border-radius:14px;background:#fff;color:#241f38;outline:none}
.rninput:focus{border-color:#7c6cf0}
.modal-bg{position:fixed;inset:0;background:rgba(30,20,50,.32);display:none;align-items:center;justify-content:center;z-index:10;padding:20px}
.modal-bg.on{display:flex}
.modal{background:#fff;border-radius:16px;padding:20px 20px 16px;width:100%;max-width:340px;box-shadow:0 12px 40px rgba(30,20,50,.28)}
.modal h2{font-size:15px;font-weight:800;margin-bottom:8px;letter-spacing:-.01em}
.modal p{font-size:12.5px;color:#6b6580;line-height:1.5;margin-bottom:14px}
.modal .row{display:flex;gap:9px}
.modal .btn{flex:1}
</style></head><body>` + body + `
<div class="modal-bg" id="mcsModal" onclick="if(event.target===this) closeConfirm()">
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="mcsModalTitle" aria-describedby="mcsModalBody">
    <h2 id="mcsModalTitle">Switch account?</h2>
    <p id="mcsModalBody">This will quit the running Claude and reopen it with the new account. Any in-progress work will be interrupted.</p>
    <div class="row">
      <button class="btn btn-light" onclick="closeConfirm()">Cancel</button>
      <button class="btn btn-primary" id="mcsModalOk" onclick="okConfirm()">Switch</button>
    </div>
  </div>
</div>
<script>
  function send(a,arg){ window.webkit.messageHandlers.mcs.postMessage({action:a, folder:arg||''}); }
  function toggleCard(el){ el.classList.toggle('selected'); var c=el.querySelector('.chk'); if(c) c.checked=el.classList.contains('selected'); }
  function confirmManaged(){
    var picked=[];
    document.querySelectorAll('.card.selectable.selected').forEach(function(c){ if(c.dataset.folder) picked.push(c.dataset.folder); });
    send('confirmManaged', JSON.stringify(picked));
  }
  function syncDir(f,t){ send('sync', f+'|'+t); }
  function renameSave(f){ var v=document.getElementById('rn').value.trim(); send('renameSave', JSON.stringify([f, v])); }
  var _pendingSwitch=null;
  function askSwitch(folder, name){
    _pendingSwitch=folder;
    document.getElementById('mcsModalTitle').textContent='Switch to '+name+'?';
    document.getElementById('mcsModal').classList.add('on');
    document.getElementById('mcsModalOk').focus();
  }
  function closeConfirm(){ _pendingSwitch=null; document.getElementById('mcsModal').classList.remove('on'); }
  function okConfirm(){ var f=_pendingSwitch; closeConfirm(); if(f) send('switch', f); }
  // Enter is intentionally NOT hijacked: browsers activate the focused button on Enter,
  // so tabbing to Cancel and pressing Enter cancels — hijacking it would silently confirm.
  document.addEventListener('keydown', function(e){
    if(e.key==='Escape' && document.getElementById('mcsModal').classList.contains('on')) closeConfirm();
  });
</script></body></html>`
}

func avatarHeader(title, subtitle string) string {
	return `<div class="header">
  <div class="avatar"><svg viewBox="0 0 24 24"><circle cx="8" cy="12" r="3.2" fill="#fff"/><circle cx="16" cy="12" r="3.2" fill="#fff"/><circle cx="8" cy="12" r="1.3" fill="#7c6cf0"/><circle cx="16" cy="12" r="1.3" fill="#7c6cf0"/></svg></div>
  <div class="htext"><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(subtitle) + `</p></div>
</div>`
}

// renderList is the account-list view: click a card to switch, Rescan / Quit in
// the footer.
func renderList(profiles []profileVM) string {
	esc := html.EscapeString
	var cards strings.Builder
	for _, p := range profiles {
		badge := planPill(p.Plan)
		editBtn := fmt.Sprintf(`<button class="edit" data-folder="%s" onclick="event.stopPropagation();send('showRename',this.dataset.folder)">✎</button>`, esc(p.Folder))
		if p.Current {
			cards.WriteString(fmt.Sprintf(`
      <div class="card current"><div class="dotcur"></div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div><div class="sub">Current account</div></div>%s</div>`,
				esc(p.Name), badge, editBtn))
			continue
		}
		cards.WriteString(fmt.Sprintf(`
      <div class="card selectable" data-folder="%s" data-name="%s" onclick="askSwitch(this.dataset.folder,this.dataset.name)"><div class="chev">⇄</div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div><div class="sub">Switch to this account</div></div>%s</div>`,
			esc(p.Folder), esc(p.Name), esc(p.Name), badge, editBtn))
	}
	if len(profiles) == 0 {
		cards.WriteString(`<div class="empty">No managed accounts yet. Run Rescan to add some.</div>`)
	}
	body := avatarHeader("Multi-Claude Switcher", "Switch or manage your Claude accounts") +
		`<div class="cards">` + cards.String() + `</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showRescan','')">⟳&nbsp; Rescan</button>
  <button class="btn btn-light" onclick="send('showSettings','')">⚙&nbsp; Settings</button>
</div>`
	return shell(body)
}

// settingsVM holds the state shown in the Settings view.
type settingsVM struct {
	AutoSync   bool
	StartLogin bool
	Version    string
	Status     string // transient feedback banner (e.g. after a backup)
	Busy       bool   // a maintenance action is running; disable its button
}

func toggleClass(on bool) string {
	if on {
		return "toggle on"
	}
	return "toggle"
}

// renderSettings is the in-panel Settings view: preferences and maintenance,
// reached from the gear on the account list. Back arrow returns to the list.
func renderSettings(vm settingsVM) string {
	status := ""
	if vm.Status != "" {
		status = `<div class="status">` + html.EscapeString(vm.Status) + `</div>`
	}
	// While a maintenance action runs, disable both maintenance buttons; the
	// status banner says which one is in progress.
	dis := ""
	on := func(action string) string { return `onclick="send('` + action + `','')"` }
	if vm.Busy {
		dis = " disabled"
		on = func(string) string { return "" }
	}
	backupBtn := `<button class="sbtn"` + dis + ` ` + on("backup") + `>Back up all accounts</button>`
	updateBtn := `<button class="sbtn"` + dis + ` ` + on("checkUpdates") + `>Check for updates…</button>`
	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Settings</h1><p>Preferences and maintenance</p></div>
</div>` + status + `
<div class="slist">
  <div class="srow"><div class="slabel"><div class="t">Auto Sync on switch</div><div class="s">Merge both accounts' Code sessions when switching</div></div>
    <div class="` + toggleClass(vm.AutoSync) + `" onclick="send('toggleAutoSync','')"></div></div>
  <div class="srow"><div class="slabel"><div class="t">Start at login</div><div class="s">Launch Multi-Claude Switcher when you log in</div></div>
    <div class="` + toggleClass(vm.StartLogin) + `" onclick="send('toggleLogin','')"></div></div>
  <button class="sbtn" onclick="send('showSync','')">Sync sessions…</button>
  ` + backupBtn + `
  ` + updateBtn + `
  <button class="sbtn" onclick="send('openLog','')">Open log folder</button>
  <button class="sbtn" onclick="send('openBackups','')">Open backup folder</button>
  <button class="sbtn danger" onclick="send('quit','')">Quit Multi-Claude Switcher</button>
</div>
<div class="about">Multi-Claude Switcher ` + html.EscapeString(vm.Version) + `</div>`
	return shell(body)
}

// renderRescan is the in-panel Rescan view: check the accounts to manage. Ghost
// accounts are shown read-only. Cancel / Confirm in the footer. No separate
// window — this replaces the panel content and swaps back on confirm.
func renderRescan(accounts []core.ScannedAccount, preselected map[string]bool) string {
	esc := html.EscapeString
	var cards strings.Builder
	for _, a := range accounts {
		date := "—"
		if !a.LastUpdated.IsZero() {
			date = a.LastUpdated.Format("2006-01-02")
		}
		if !a.Complete {
			cards.WriteString(fmt.Sprintf(`
      <div class="card ghost"><div style="width:21px;flex:none"></div>
        <div class="body"><div class="row1"><span class="name">Unrecognized account</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-bad">%s</div></div></div>`,
				esc(shortID(a.UUID)), a.Convos, esc(date), esc(a.Note)))
			continue
		}
		name := a.Email
		if name == "" {
			name = a.HomeFolder
		}
		badge := ""
		switch a.Account {
		case core.AccountTeam:
			badge = `<span class="pill pill-team">🏢 Team</span>`
		case core.AccountPersonal:
			badge = `<span class="pill pill-personal">Personal</span>`
		}
		sel, chk := "", ""
		if preselected[a.HomeFolder] {
			sel, chk = " selected", " checked"
		}
		cards.WriteString(fmt.Sprintf(`
      <div class="card selectable%s" data-folder="%s" onclick="toggleCard(this)">
        <input type="checkbox" class="chk"%s>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div></div></div>`,
			sel, esc(a.HomeFolder), chk, esc(name), badge, esc(shortID(a.UUID)), a.Convos, esc(date)))
	}
	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Choose accounts</h1><p>Which accounts should be managed?</p></div>
</div>
<div class="cards">` + cards.String() + `</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" onclick="confirmManaged()">Confirm</button>
</div>`
	return shell(body)
}

// renderRename is the in-panel Rename view: a text field for a friendlier
// display name for one account.
func renderRename(folder, current string) string {
	esc := html.EscapeString
	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Rename account</h1><p>Give this account a friendlier name</p></div>
</div>
<input id="rn" class="rninput" type="text" value="` + esc(current) + `" placeholder="Display name">
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" data-folder="` + esc(folder) + `" onclick="renameSave(this.dataset.folder)">Save</button>
</div>
<script>var e=document.getElementById('rn'); e.focus(); e.select();</script>`
	return shell(body)
}

// renderSync is the in-panel Sync view: one card per direction (From → To).
// Tapping a direction copies that account's Code sessions into the other.
func renderSync(profiles []profileVM, status string, busy bool) string {
	esc := html.EscapeString
	st := ""
	if status != "" {
		st = `<div class="status">` + esc(status) + `</div>`
	}
	var cards strings.Builder
	count := 0
	for _, from := range profiles {
		for _, to := range profiles {
			if from.Folder == to.Folder {
				continue
			}
			count++
			note := ""
			if to.Plan == "Team" {
				note = `<div class="sub" style="color:#b0455f">Team destination — import won't apply</div>`
			}
			oc, cls := "", "card selectable"
			if busy {
				cls = "card ghost"
			} else {
				oc = fmt.Sprintf(`data-from="%s" data-to="%s" onclick="syncDir(this.dataset.from,this.dataset.to)"`, esc(from.Folder), esc(to.Folder))
			}
			cards.WriteString(fmt.Sprintf(`
      <div class="%s" %s><div class="chev">→</div>
        <div class="body"><div class="row1"><span class="name">%s → %s</span></div>
          <div class="sub">Copy %s's sessions into %s</div>%s</div></div>`,
				cls, oc, esc(from.Name), esc(to.Name), esc(from.Name), esc(to.Name), note))
		}
	}
	if count == 0 {
		cards.WriteString(`<div class="empty">Add at least two managed accounts to sync between them.</div>`)
	}
	body := `<div class="header">
  <button class="back" onclick="send('showSettings','')">‹</button>
  <div class="htext"><h1>Sync sessions</h1><p>Copy Code sessions between accounts</p></div>
</div>` + st + `
<div class="cards">` + cards.String() + `</div>`
	return shell(body)
}

// computePreselect returns the folders to pre-check in the Rescan view.
func computePreselect(accounts []core.ScannedAccount, managed []string) map[string]bool {
	firstRun := managed == nil
	set := map[string]bool{}
	for _, m := range managed {
		set[m] = true
	}
	pre := map[string]bool{}
	for _, a := range accounts {
		if a.Complete && (firstRun || set[a.HomeFolder]) {
			pre[a.HomeFolder] = true
		}
	}
	return pre
}

func shortID(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}
