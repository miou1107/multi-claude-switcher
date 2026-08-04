// Package panelui renders the WebView-hosted account panel that is shared by
// the macOS menu-bar host (cmd/mcs-menubar, WKWebView in an NSPopover) and the
// Windows tray host (cmd/mcs-tray, jchv/go-webview2 in a borderless topmost
// window). Both hosts consume the exact same HTML/CSS/JS output so the UI
// stays in lockstep across platforms.
//
// The JS bridge is feature-detected inside the shared shell(): mac has
// window.webkit.messageHandlers.mcs, Windows has window.mcsAction bound by
// go-webview2's Bind API.
package panelui

import (
	"fmt"
	"html"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/core/diagnostics"
)

// ProfileVM is one row in the account-list view.
type ProfileVM struct {
	Folder  string
	Name    string
	Plan    string // subscription label: "Team" | "Max 20×" | "Pro" | "Free" | …
	Current bool

	// SignedIn is false for a profile folder that exists but has no account in
	// it yet. It can still be switched to, which is how the user signs in, but
	// it cannot take part in a sync: sessions are stored per account, so with
	// no account there is no bucket to read from or write to.
	SignedIn bool

	// Convos is how many Code conversations this profile holds for its own
	// account. The sync confirmation quotes it, so the user can see how much is
	// about to move before agreeing to have Claude closed.
	Convos int

	// UUID is the account signed in to this profile, empty when none is. The
	// account list groups by it to spot two profiles holding one account, which
	// is a state the user has to resolve.
	UUID string
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
// same webview — there are no separate windows on either platform.
func shell(body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Multi-Claude Switcher</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html{color-scheme:light}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","SF Pro Text",system-ui,sans-serif;color:#241f38;
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
.chip{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:10.5px;background:#f1eef9;color:#6b6580;padding:2px 6px;border-radius:5px}
.dot{opacity:.5}
.pill{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;white-space:nowrap}
.pill-team{background:#d6f5e3;color:#1a8a4f}
.pill-personal{background:#eceaf3;color:#6b6580}
.pill-plan{background:#ece8fb;color:#6a4fd0}
.addcard{display:flex;align-items:center;justify-content:center;gap:7px;background:transparent;border:2px dashed #cdc8e0;border-radius:14px;padding:13px 14px;cursor:pointer;font:inherit;font-size:13px;font-weight:800;color:#6b6580;width:100%}
.addcard:hover{border-color:#7c6cf0;color:#7c6cf0;background:#faf9ff}
.note-bad{margin-top:5px;display:inline-block;font-size:10.5px;font-weight:700;background:#fde4e4;color:#c0392b;padding:2px 8px;border-radius:999px}
.dup{background:#fde4e4;border-radius:12px;padding:11px 13px;margin-bottom:11px;display:flex;align-items:center;gap:10px}
.dup .dt{flex:1;font-size:12px;color:#a32d2d;line-height:1.45}
.dup-pill{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;background:#fde4e4;color:#a32d2d;white-space:nowrap}
.btn-sm{font:inherit;font-size:12px;font-weight:700;border:none;cursor:pointer;border-radius:9px;padding:7px 12px;background:linear-gradient(135deg,#7c6cf0,#9b6bff);color:#fff;flex:none}
.btn-sm:hover{filter:brightness(1.05)}
.note-todo{margin-top:5px;display:inline-block;font-size:10.5px;font-weight:700;background:#e6eefc;color:#2b62c9;padding:2px 8px;border-radius:999px;white-space:normal}
.empty{color:#8b8598;font-size:13px;text-align:center;padding:18px 8px}
.footer{display:flex;gap:9px;margin-top:14px}
.btn{flex:1;font:inherit;font-weight:700;font-size:13px;border:none;cursor:pointer;border-radius:11px;padding:10px;transition:.13s}
.btn-light{background:#fff;color:#514b66;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.btn-light:hover{background:#f6f4fb}
.btn-primary{background:linear-gradient(135deg,#7c6cf0,#9b6bff);color:#fff;box-shadow:0 4px 12px rgba(124,108,240,.4)}
.btn-primary:hover{filter:brightness(1.05)}
/* The confirm button of a destructive dialog, and only that one. Every other
   dialog keeps btn-primary: red on all of them would read as "this is the
   confirm button" rather than "this one takes something away". */
.btn-danger{background:linear-gradient(135deg,#d5566d,#c0392b);color:#fff;box-shadow:0 4px 12px rgba(192,57,43,.35)}
.btn-danger:hover{filter:brightness(1.05)}
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
.sbtn:disabled{opacity:.5;cursor:default;color:#8b8598}
.sbtn:disabled:hover{background:#fff}
/* Red text inside a red border, so the one button on the account screen that
   takes something away does not read as another row of settings. It sits below
   a rule and away from Save, which is the other half of the delete-button rule
   this follows. */
.sbtn.danger{color:#b0455f;border:1.5px solid #e0a3b1}
.sbtn.danger:hover{background:#fdf2f5}
.sbtn.danger:disabled{border-color:#ded9ec}
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
.hint{font-size:12px;color:#6b6580;line-height:1.5;margin-top:11px}
.dangerzone{border-top:1px solid #ece9f4;margin-top:18px;padding-top:14px}
.hintw{background:#fff6e0;color:#854f0b;font-size:12px;line-height:1.5;padding:9px 12px;border-radius:11px;margin-top:10px}
.hintw .noteline+.noteline{margin-top:7px}
.dbgnote{background:#e9f5ee;color:#1a7a3d;font-size:11.5px;line-height:1.5;padding:9px 12px;border-radius:11px;margin-bottom:9px}
.dbgbox{background:#fff;border-radius:12px;padding:11px 12px;font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:10.5px;line-height:1.65;color:#514b66;max-height:210px;overflow:auto;white-space:pre-wrap;word-break:break-word}
.dbgarea{width:100%;height:60px;font:inherit;font-size:12px;padding:10px 12px;border:2px solid #e0dcf3;border-radius:12px;background:#fff;color:#241f38;outline:none;resize:none}
.dbgarea:focus{border-color:#7c6cf0}
.errbox{background:#fde4e4;color:#a32d2d;font-size:12px;font-weight:700;padding:9px 12px;border-radius:11px;margin-bottom:11px}
.modal-bg{position:fixed;inset:0;background:rgba(30,20,50,.32);display:none;align-items:center;justify-content:center;z-index:10;padding:20px}
.modal-bg.on{display:flex}
.modal{background:#fff;border-radius:16px;padding:20px 20px 16px;width:100%;max-width:340px;box-shadow:0 12px 40px rgba(30,20,50,.28)}
.modal h2{font-size:15px;font-weight:800;margin-bottom:8px;letter-spacing:-.01em}
.modal p{font-size:12.5px;color:#6b6580;line-height:1.5;margin-bottom:14px}
.modal .warn{background:#fff6e0;color:#854f0b;font-size:12px;line-height:1.45;padding:8px 11px;border-radius:10px;margin-bottom:14px}
.modal .row{display:flex;gap:9px}
.modal .btn{flex:1}
</style></head><body>` + body + `
<div class="modal-bg" id="mcsModal" onclick="if(event.target===this) closeConfirm()">
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="mcsModalTitle" aria-describedby="mcsModalBody">
    <h2 id="mcsModalTitle">Close Claude?</h2>
    <p id="mcsModalBody"></p>
    <div class="warn">Anything unsaved in Claude is interrupted.</div>
    <div class="row">
      <button class="btn btn-light" id="mcsModalCancel" onclick="closeConfirm()">Cancel</button>
      <button class="btn btn-primary" id="mcsModalOk" onclick="okConfirm()">Continue</button>
    </div>
  </div>
</div>
<script>
  // Bridge: mac hosts webkit.messageHandlers.mcs; Windows hosts a Go function
  // bound as window.mcsAction via go-webview2's Bind API. Feature-detect once.
  function send(a, arg){
    if (window.mcsAction) { window.mcsAction(a, arg || ''); return; }
    window.webkit.messageHandlers.mcs.postMessage({action:a, folder:arg||''});
  }
  function toggleCard(el){ el.classList.toggle('selected'); var c=el.querySelector('.chk'); if(c) c.checked=el.classList.contains('selected'); }
  function confirmManaged(){
    var picked=[];
    document.querySelectorAll('.card.selectable.selected').forEach(function(c){ if(c.dataset.folder) picked.push(c.dataset.folder); });
    send('confirmManaged', JSON.stringify(picked));
  }
  function renameSave(f){ var v=document.getElementById('rn').value.trim(); send('renameSave', JSON.stringify([f, v])); }
  function createProfileSave(btn){
    var v=document.getElementById('np').value.trim();
    send('createProfile', JSON.stringify([v, btn.dataset.uuid||'']));
  }
  function toggleMergePick(el){
    var cards=el.parentNode.querySelectorAll('.card.selectable');
    for(var i=0;i<cards.length;i++){
      var on=cards[i]===el;
      cards[i].classList.toggle('selected',on);
      var c=cards[i].querySelector('.chk'); if(c) c.checked=on;
    }
  }
  function mergeConfirm(){
    var sel=document.querySelector('.card.selectable.selected');
    var all=document.querySelectorAll('.card.selectable');
    if(!sel||all.length!==2) return;
    var other=all[0]===sel?all[1]:all[0];
    send('mergeConfirm', sel.dataset.folder+'|'+other.dataset.folder);
  }

  // Every action that closes the user's Claude goes through askConfirm. Nothing
  // may call send('switch') or send('sync') directly: closing an app somebody is
  // working in is not a single-click operation, and a warning that one code path
  // can skip is not a warning.
  //
  // kind is 'destructive' for a dialog that takes something away, and anything
  // else (omitted, in practice) for the rest. It only paints the confirm button:
  // switching, syncing and reporting a problem all close Claude or publish
  // something, but none of them removes an account, and a red button on every
  // dialog would stop meaning anything.
  var _pending=null;
  function askConfirm(action, arg, title, body, okLabel, warn, kind){
    _pending={a:action, arg:arg};
    document.getElementById('mcsModalTitle').textContent=title;
    document.getElementById('mcsModalBody').textContent=body;
    document.querySelector('#mcsModal .warn').textContent=
      warn || 'Anything unsaved in Claude is interrupted.';
    var ok=document.getElementById('mcsModalOk');
    // Set both ways round: the dialog is reused, so a plain confirm opened after
    // a removal must not inherit the red button.
    ok.classList.toggle('btn-danger', kind==='destructive');
    ok.classList.toggle('btn-primary', kind!=='destructive');
    document.getElementById('mcsModalOk').textContent=okLabel;
    document.getElementById('mcsModal').classList.add('on');
    // Cancel takes the focus, not Continue. Enter on an unread dialog must not
    // close somebody's app.
    document.getElementById('mcsModalCancel').focus();
  }
  function askSwitch(folder, name){
    askConfirm('switch', folder, 'Switch to '+name+'?',
      'Claude closes and reopens signed in as '+name+'.', 'Switch');
  }
  function askReport(){
    askConfirm('reportProblem', document.getElementById('dbgc').value,
      'Open a GitHub issue?',
      'The report above and your comment are copied to your clipboard, and your browser opens a new issue on the MCS repository. Paste it there and you can still edit it before submitting.',
      'Copy and open',
      'GitHub issues are public. What is copied is exactly what you saw on the screen behind this dialog, with your email address, account IDs, user name and home folder already replaced with stand-ins.');
  }
  // The folder and name travel as data-* and are read back with dataset, never
  // interpolated into the inline handler: a name with an apostrophe would
  // otherwise break the parse (the v0.9.1 bug).
  function askRemove(el){
    var n = parseInt(el.dataset.convos, 10) || 0;
    // Zero gets its own wording rather than falling through to "all 0
    // conversations": a freshly created, never-signed-in slot is the profile
    // most likely to be the one somebody removes, so this is not a rare edge.
    var what = n === 0 ? 'no conversations yet' : n === 1 ? 'its 1 conversation' : 'all ' + n + ' conversations';
    askConfirm('removeProfile', el.dataset.folder, 'Remove '+el.dataset.name+'?',
      'It disappears from the switcher. Its folder, with '+what+', moves to the archive folder you can open from Settings.',
      'Remove',
      'To use this account again you have to sign in to it again.',
      'destructive');
  }
  // The folders arrive via data-* and are joined here, never interpolated into the
  // inline handler — a folder with an apostrophe would otherwise break the parse.
  function mergePair(a, b){ send('showMerge', a+'|'+b); }
  function askSync(from, to, fromName, toName, convos){
    var n = parseInt(convos, 10) || 0;
    var what = n === 1 ? '1 conversation is copied' : n + ' conversations are copied';
    askConfirm('sync', from+'|'+to, 'Sync into '+toName+'?',
      'Claude closes, '+what+' from '+fromName+', then Claude reopens where you were.', 'Sync');
  }
  function closeConfirm(){ _pending=null; document.getElementById('mcsModal').classList.remove('on'); }
  function okConfirm(){ var p=_pending; closeConfirm(); if(p) send(p.a, p.arg); }
  // Enter is intentionally NOT hijacked: browsers activate the focused button on Enter,
  // so tabbing to Cancel and pressing Enter cancels — hijacking it would silently confirm.
  document.addEventListener('keydown', function(e){
    if(e.key!=='Escape') return;
    if(document.getElementById('mcsModal').classList.contains('on')) { closeConfirm(); return; }
    // Inside a text input (Rename), Esc backs out to the list instead of
    // killing the panel — hiding on Windows would discard the typed name.
    var ae=document.activeElement;
    // The Debug comment box is the one exception: showList jumps past
    // Settings, which the back button does not, and — same as pressing that
    // back button used to — discarded whatever the user had typed, since it
    // was never sent to Go until Copy or Report a problem. Sending it as the
    // arg here mirrors the back button's fix: showSettings on the Go side
    // saves it before switching the view away.
    if(ae && ae.id==='dbgc') { send('showSettings', ae.value); return; }
    if(ae && (ae.tagName==='INPUT' || ae.tagName==='TEXTAREA')) { send('showList',''); return; }
    // Otherwise, Esc hides the whole panel (matches NSPopover click-outside).
    send('hidePanel','');
  });
</script></body></html>`
}

func avatarHeader(title, subtitle string) string {
	return `<div class="header">
  <div class="avatar"><svg viewBox="0 0 24 24"><circle cx="8" cy="12" r="3.2" fill="#fff"/><circle cx="16" cy="12" r="3.2" fill="#fff"/><circle cx="8" cy="12" r="1.3" fill="#7c6cf0"/><circle cx="16" cy="12" r="1.3" fill="#7c6cf0"/></svg></div>
  <div class="htext"><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(subtitle) + `</p></div>
</div>`
}

// RenderList is the account-list view: click a card to switch, Rescan / Quit
// in the footer.
// RenderList draws the account list.
//
// canAddAccount shows the card that starts the add-an-account flow. It is off
// where there is nothing behind it: today only the Windows Store build can create
// a profile, so offering the card elsewhere would be a button that does nothing.
// duplicateAccounts groups profiles by the account signed in to them and reports
// the ones sharing a single account: two profiles holding one account is a state
// to resolve, not a preference. It returns the set of folders to flag on the list,
// and the warning banner offering the merge for the first such group only — each
// merge needs Claude quit, so batching them would only lengthen the same
// interruption. Profiles with no account yet (empty UUID) are never grouped.
func duplicateAccounts(profiles []ProfileVM, esc func(string) string) (map[string]bool, string) {
	byUUID := map[string][]string{}
	nameByFolder := map[string]string{}
	var uuidOrder []string
	for _, p := range profiles {
		nameByFolder[p.Folder] = p.Name
		if p.UUID == "" {
			continue // no account signed in yet; nothing to be a duplicate of
		}
		if _, ok := byUUID[p.UUID]; !ok {
			uuidOrder = append(uuidOrder, p.UUID)
		}
		byUUID[p.UUID] = append(byUUID[p.UUID], p.Folder)
	}
	dupFolder := map[string]bool{}
	var firstGroup []string
	for _, u := range uuidOrder {
		g := byUUID[u]
		if len(g) < 2 {
			continue
		}
		for _, f := range g {
			dupFolder[f] = true
		}
		if firstGroup == nil {
			firstGroup = g
		}
	}
	if firstGroup == nil {
		return dupFolder, ""
	}
	a, b := firstGroup[0], firstGroup[1]
	nameOf := func(folder string) string {
		if n, ok := nameByFolder[folder]; ok {
			return n
		}
		return folder
	}
	// Two folders can carry the same display name; naming both "Claude" in the
	// warning would read as "Claude and Claude are the same account". When the names
	// collide, fall back to the folder, which is always distinct.
	labelA, labelB := nameOf(a), nameOf(b)
	if labelA == labelB {
		labelA, labelB = a, b
	}
	// The two folder names travel as data-* and are read back with dataset.
	// Interpolating them into the onclick string would reintroduce the v0.9.1 bug:
	// html.EscapeString turns an apostrophe into &#39;, the HTML parser decodes it
	// back to ' before the JS is parsed, and the handler breaks on any folder
	// containing one.
	warning := fmt.Sprintf(`<div class="dup">
  <div class="dt">%s and %s are the same account. Merge them to clean this up.</div>
  <button class="btn-sm" data-dup-a="%s" data-dup-b="%s" onclick="mergePair(this.dataset.dupA,this.dataset.dupB)">Merge</button>
</div>`, esc(labelA), esc(labelB), esc(a), esc(b))
	return dupFolder, warning
}

func RenderList(profiles []ProfileVM, canAddAccount bool, status string) string {
	esc := html.EscapeString
	dupFolder, dupWarning := duplicateAccounts(profiles, esc)
	// status carries the result of an action that ended back on the list — a
	// merge that could not be computed, a recovery that came too late, a merge that
	// succeeded. Without it those paths re-render an unchanged-looking list and read
	// as having done nothing.
	statusBanner := ""
	if status != "" {
		statusBanner = `<div class="status">` + esc(status) + `</div>`
	}
	var cards strings.Builder
	for _, p := range profiles {
		badge := planPill(p.Plan)
		dupPill := ""
		if dupFolder[p.Folder] {
			dupPill = `<span class="dup-pill">Duplicate</span>`
		}
		editBtn := fmt.Sprintf(`<button class="edit" data-folder="%s" onclick="event.stopPropagation();send('showRename',this.dataset.folder)">✎</button>`, esc(p.Folder))
		if p.Current {
			cards.WriteString(fmt.Sprintf(`
      <div class="card current"><div class="dotcur"></div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s%s</div><div class="sub">Current account</div></div>%s</div>`,
				esc(p.Name), badge, dupPill, editBtn))
			continue
		}
		// A profile with no account yet is still switchable: switching to it is
		// how the user gets Claude open on it to sign in. Say so, otherwise the
		// card looks identical to a ready account and switching to it lands on
		// a login screen with no explanation.
		sub := "Switch to this account"
		if !p.SignedIn {
			sub = "Not signed in yet. Switch here, then sign in."
		}
		cards.WriteString(fmt.Sprintf(`
      <div class="card selectable" data-folder="%s" data-name="%s" onclick="askSwitch(this.dataset.folder,this.dataset.name)"><div class="chev">⇄</div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s%s</div><div class="sub">%s</div></div>%s</div>`,
			esc(p.Folder), esc(p.Name), esc(p.Name), badge, dupPill, esc(sub), editBtn))
	}
	if len(profiles) == 0 {
		cards.WriteString(`<div class="empty">No managed accounts yet. Run Rescan to add some.</div>`)
	}
	if canAddAccount {
		// In the list rather than the footer: the footer already holds Rescan and
		// Settings, and a third labelled button does not fit in 400px without
		// shrinking the other two to bare icons.
		cards.WriteString(`
      <button class="addcard" onclick="send('newProfile','')">＋&nbsp; Add another account</button>`)
	}
	body := avatarHeader("Multi-Claude Switcher", "Switch or manage your Claude accounts") +
		statusBanner + dupWarning + `<div class="cards">` + cards.String() + `</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showRescan','')">⟳&nbsp; Rescan</button>
  <button class="btn btn-light" onclick="send('showSettings','')">⚙&nbsp; Settings</button>
</div>
<div class="about">v` + esc(core.Version) + `</div>`
	return shell(body)
}

// SettingsVM holds the state shown in the Settings view.
type SettingsVM struct {
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

// RenderSettings is the in-panel Settings view: preferences and maintenance,
// reached from the gear on the account list. Back arrow returns to the list.
func RenderSettings(vm SettingsVM) string {
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
  <button class="sbtn" onclick="send('openArchive','')">Open archive folder</button>
  <button class="sbtn" onclick="send('showDebug','')">Debug info…</button>
  <button class="sbtn danger" onclick="send('quit','')">Quit Multi-Claude Switcher</button>
</div>
<div class="about">v` + html.EscapeString(vm.Version) + `</div>`
	return shell(body)
}

// DebugVM is the Debug info view: what MCS knows about this machine, already
// masked, and a box to say what went wrong.
//
// There used to be a Gathering flag here, for the window between showDebug
// clearing the report cache and the background gather filling it back in.
// That window no longer reaches this view at all: showDebug now gathers
// first, while Settings shows a busy banner, and only switches to this view
// once the report is ready — so RenderDebug is never asked to draw a report
// that has not finished gathering, and there is nothing left for a
// placeholder to guard against.
type DebugVM struct {
	Report  string
	Comment string
	Status  string // transient feedback, e.g. after Copy
}

// RenderDebug shows the report before it goes anywhere.
//
// There is no unmask switch and no "include the log" checkbox, so what is on
// screen is exactly what is copied — one version of the truth, and no way to
// publish something the user was not shown.
func RenderDebug(vm DebugVM) string {
	esc := html.EscapeString
	status := ""
	if vm.Status != "" {
		status = `<div class="status">` + esc(vm.Status) + `</div>`
	}
	reportBox := `<div class="dbgbox">` + esc(vm.Report) + `</div>`
	body := `<div class="header">
  <button class="back" onclick="send('showSettings', document.getElementById('dbgc').value)">‹</button>
  <div class="htext"><h1>Debug info</h1><p>Exactly what a report contains</p></div>
</div>` + status + `
<div class="dbgnote">Your email address, account IDs, user name and home folder are replaced with stand-ins like account-1 below. A name you gave a profile folder yourself can still show. ` + esc(diagnostics.UnregisteredMarker) + ` marks something that looked like an address or an ID and was blocked.</div>
` + reportBox + `
<div class="hint">What went wrong? (optional)</div>
<textarea class="dbgarea" id="dbgc" placeholder="Switching to my work account left the personal one closed…">` + esc(vm.Comment) + `</textarea>
<div class="footer">
  <button class="btn btn-light" style="flex:none;padding:10px 14px" onclick="send('copyDebug', document.getElementById('dbgc').value)">Copy</button>
  <button class="btn btn-primary" onclick="askReport()">Report a problem</button>
</div>`
	return shell(body)
}

// RenderRescan is the in-panel Rescan view: check the accounts to manage.
// Ghost accounts are shown read-only. Cancel / Confirm in the footer. No
// separate window — this replaces the panel content and swaps back on confirm.
func RenderRescan(accounts []core.ScannedAccount, preselected map[string]bool) string {
	esc := html.EscapeString
	var cards strings.Builder
	for _, a := range accounts {
		date := "No date yet"
		if !a.LastUpdated.IsZero() {
			date = a.LastUpdated.Format("2006-01-02")
		}
		if a.SignedOut || a.Pending {
			// A profile folder with no account in it yet. Selectable on
			// purpose: managing it is what puts it in the account list, which
			// is how the user gets to switch to it and sign in.
			//
			// Pending rows — profiles MCS has just made and told the user to go
			// and sign in to — belong here rather than with the ghosts below.
			// They have no account UUID, so the ghost branch claimed them and
			// drew them as "Unrecognized account": no name, no tick box, and a
			// warning about a folder the user had asked for thirty seconds
			// earlier.
			sel, chk := "", ""
			if preselected[a.HomeFolder] {
				sel, chk = " selected", " checked"
			}
			cards.WriteString(fmt.Sprintf(`
      <div class="card selectable%s" data-folder="%s" onclick="toggleCard(this)">
        <input type="checkbox" class="chk"%s>
        <div class="body"><div class="row1"><span class="name">%s</span></div>
          <div class="note-todo">%s</div></div></div>`,
				sel, esc(a.HomeFolder), chk, esc(a.HomeFolder), esc(a.Note)))
			continue
		}
		if !a.Complete {
			if a.Recoverable {
				// Not selectable: there is no folder to manage yet. The action is
				// to give this account one, which is what Recover does. The note
				// is deliberately blue — nothing here is broken, the conversations
				// are intact and only the profile is missing. The action carries
				// only the UUID: the source paths are re-read from a fresh scan
				// when recovery runs, since they are valid only for the scan that
				// produced them.
				cards.WriteString(fmt.Sprintf(`
      <div class="card"><div style="width:21px;flex:none"></div>
        <div class="body"><div class="row1"><span class="name">Signed out in Claude Desktop</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-todo">%s</div></div>
        <button class="btn-sm" data-uuid="%s" onclick="send('showRecover',this.dataset.uuid)">Recover</button></div>`,
					esc(ShortID(a.UUID)), a.Convos, esc(date), esc(a.Note),
					esc(a.UUID)))
				continue
			}
			cards.WriteString(fmt.Sprintf(`
      <div class="card ghost"><div style="width:21px;flex:none"></div>
        <div class="body"><div class="row1"><span class="name">Unrecognized account</span></div>
          <div class="meta"><span class="chip">%s</span><span class="dot">·</span>%d chats<span class="dot">·</span>%s</div>
          <div class="note-bad">%s</div></div></div>`,
				esc(ShortID(a.UUID)), a.Convos, esc(date), esc(a.Note)))
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
			sel, esc(a.HomeFolder), chk, esc(name), badge, esc(ShortID(a.UUID)), a.Convos, esc(date)))
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

// AccountVM drives the per-account screen: renaming, and removal.
type AccountVM struct {
	Folder  string // the identity, the key every action carries
	Name    string // display name
	Convos  int    // conversations in its own account bucket
	Current bool   // Claude is running on it: remove is disabled
	OnlyOne bool   // the only profile listed: remove is hidden
}

// RenderAccount is the in-panel screen reached from the pencil on an account row.
// It was the Rename screen; removal lives at the bottom of it rather than as a bin
// icon beside the pencil, because two small adjacent icons is the arrangement most
// likely to be mis-tapped, and the delete-button rule is red and away from edit.
//
// Disabling for Current is a courtesy, not the guard. core.RemoveProfile asks what
// Claude has open at the moment of the action, because this screen may have been
// drawn before the user opened Claude on the account it is about.
func RenderAccount(vm AccountVM) string {
	esc := html.EscapeString

	remove := ""
	if !vm.OnlyOne {
		btn := fmt.Sprintf(`<button class="sbtn danger" data-folder="%s" data-name="%s" data-convos="%d" onclick="askRemove(this)">Remove this account</button>`,
			esc(vm.Folder), esc(vm.Name), vm.Convos)
		note := `<div class="hint">Removing takes this account off the list. Its folder is archived, not deleted.</div>`
		if vm.Current {
			btn = `<button class="sbtn danger" disabled>Remove this account</button>`
			note = `<div class="hint">Switch to another account first, then you can remove it.</div>`
		}
		remove = `<div class="dangerzone">` + note + btn + `</div>`
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Account settings</h1><p>Rename or remove this account</p></div>
</div>
<input id="rn" class="rninput" type="text" value="` + esc(vm.Name) + `" placeholder="Display name">
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" data-folder="` + esc(vm.Folder) + `" onclick="renameSave(this.dataset.folder)">Save</button>
</div>` + remove + `
<script>var e=document.getElementById('rn'); e.focus(); e.select();</script>`
	return shell(body)
}

// NewProfileVM drives the name-the-profile screen. One screen serves both
// entry points: RecoverUUID empty is the plain add path, set is a recovery of
// that account. They run the same underlying operation and differ only in copy
// and in whether a session bucket comes along, so sharing the view is what keeps
// the two from drifting apart.
type NewProfileVM struct {
	RecoverUUID   string
	SuggestedName string
	Convos        int
	Err           string
}

// RenderNewProfile is the in-panel screen for naming a new account profile.
func RenderNewProfile(vm NewProfileVM) string {
	esc := html.EscapeString
	recovering := vm.RecoverUUID != ""

	title, sub, confirm := "Add another account", "It gets its own profile", "Add"
	if recovering {
		title, sub, confirm = "Recover this account", "It gets its own profile", "Recover"
	}

	// "different account" stays one unbroken phrase inside the <b>, so a test can
	// assert on it as the user reads it. ShortID is the account's first 8 characters,
	// so the copy says "starting", not "ending".
	second := `<div class="hintw">Sign in as a <b>different account</b>. Signing in as one you already have creates a duplicate, and MCS will ask you to merge.</div>`
	if recovering {
		second = fmt.Sprintf(`<div class="hintw">Sign in as the account starting <b>%s</b> (%d chats). Its conversations come back on their own.</div>`,
			esc(ShortID(vm.RecoverUUID)), vm.Convos)
	}

	errBlock := ""
	if vm.Err != "" {
		errBlock = `<div class="errbox">` + esc(vm.Err) + `</div>`
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>` + esc(title) + `</h1><p>` + esc(sub) + `</p></div>
</div>` + errBlock + `
<input id="np" class="rninput" type="text" value="` + esc(vm.SuggestedName) + `" placeholder="Personal">
<div class="hint">Claude closes, your current account is saved, and a clean Claude opens.</div>` + second + `
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary" data-uuid="` + esc(vm.RecoverUUID) + `" onclick="createProfileSave(this)">` + esc(confirm) + `</button>
</div>
<script>var e=document.getElementById('np'); e.focus(); e.select();</script>`
	return shell(body)
}

// MergeCandidateVM is one side of a duplicate pair on the merge screen.
type MergeCandidateVM struct {
	Folder  string
	Name    string
	Plan    string
	Convos  int
	Current bool // the profile Claude is running on right now
}

// RenderMerge is the in-panel screen for resolving two profiles signed in to one
// account. The profile in use is preselected to keep, because keeping it means
// the user does not have to sign in again; they can pick the other one when they
// prefer its name. Conversations are combined either way, so the choice only
// decides which name survives.
//
// plan is what the merge will actually do, computed by core.MergePreview before
// this screen is rendered. The total shown comes from there and not from adding the
// two counts: a conversation both profiles hold is one conversation afterwards, not
// two.
func RenderMerge(a, b MergeCandidateVM, plan core.MergePlan, status string, busy bool) string {
	esc := html.EscapeString
	st := ""
	if status != "" {
		st = `<div class="status">` + esc(status) + `</div>`
	}
	// Keep the in-use profile by default. If neither is running, fall back to the
	// first, so the screen is never rendered with nothing chosen.
	keep := a.Folder
	if b.Current && !a.Current {
		keep = b.Folder
	}

	card := func(c MergeCandidateVM) string {
		cls := "card selectable"
		if c.Folder == keep {
			cls += " selected"
		}
		sub := "Will be archived"
		if c.Folder == keep {
			sub = "Keep this one"
			if c.Current {
				sub = "In use now · keep this one"
			}
		}
		return fmt.Sprintf(`
      <div class="%s" data-folder="%s" onclick="toggleMergePick(this)">
        <input type="checkbox" class="chk"%s>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div>
          <div class="meta">%d chats<span class="dot">·</span>%s</div></div></div>`,
			cls, esc(c.Folder), map[bool]string{true: " checked", false: ""}[c.Folder == keep],
			esc(c.Name), planPill(c.Plan), c.Convos, esc(sub))
	}

	dis, oc := "", `onclick="mergeConfirm()"`
	if busy {
		dis, oc = " disabled", ""
	}

	// Where both profiles hold a record and the keeper's copy is the newer one, the
	// sync leaves the keeper's alone and the archive keeps the other. Say so before
	// the user commits: after the merge that version is reachable only by opening the
	// archive folder.
	conflictNote := ""
	if plan.Conflicts > 0 {
		conflictNote = fmt.Sprintf(`<div class="hintw">%d conversations exist in both profiles and have changed since they were last in step. The newer version is kept. The other stays in the archived folder, which you can open from Settings.</div>`, plan.Conflicts)
	}
	// Say so rather than quietly delivering a smaller number than promised.
	if plan.Unreadable > 0 {
		conflictNote += fmt.Sprintf(`<div class="hintw">%d files couldn't be read and will be left where they are.</div>`, plan.Unreadable)
	}

	body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>Merge duplicates</h1><p>Both are the same account</p></div>
</div>` + st + `
<div class="cards">` + card(a) + card(b) + `</div>
<div class="hint">All ` + fmt.Sprint(plan.Combined) + ` conversations are combined into the account you keep. The other folder is archived, not deleted, so you can put it back yourself.</div>` + conflictNote + `
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Cancel</button>
  <button class="btn btn-primary"` + dis + ` ` + oc + `>Merge</button>
</div>`
	return shell(body)
}

// RenderSync is the in-panel Sync view: one card per direction (From → To).
// Tapping a direction copies that account's Code sessions into the other.
func RenderSync(profiles []ProfileVM, status string, busy bool) string {
	esc := html.EscapeString
	st := ""
	if status != "" {
		st = `<div class="status">` + esc(status) + `</div>`
	}
	var cards strings.Builder
	count := 0
	waiting := 0
	for _, p := range profiles {
		if !p.SignedIn {
			waiting++
		}
	}
	for _, from := range profiles {
		for _, to := range profiles {
			if from.Folder == to.Folder {
				continue
			}
			// Sessions are stored per account, so a profile with no account
			// signed in has no bucket to read from or write to. Offering the
			// direction anyway just fails at the point of clicking it.
			if !from.SignedIn || !to.SignedIn {
				continue
			}
			count++
			// No per-destination caveat. A Team account used to be marked as one
			// that could not receive an import; that was MCS filing conversations
			// under the wrong organization, not a limit of the account.
			oc, cls := "", "card selectable"
			if busy {
				cls = "card ghost"
			} else {
				// Through askSync, never straight to send('sync'): this closes the
				// user's Claude, so it needs their yes first. The names and count
				// ride along so the dialog can say which way the copy goes and how
				// much moves — "Sync?" alone does not tell the user that.
				oc = fmt.Sprintf(`data-from="%s" data-to="%s" data-from-name="%s" data-to-name="%s" data-convos="%d" `+
					`onclick="askSync(this.dataset.from,this.dataset.to,this.dataset.fromName,this.dataset.toName,this.dataset.convos)"`,
					esc(from.Folder), esc(to.Folder), esc(from.Name), esc(to.Name), from.Convos)
			}
			cards.WriteString(fmt.Sprintf(`
      <div class="%s" %s><div class="chev">→</div>
        <div class="body"><div class="row1"><span class="name">%s → %s</span></div>
          <div class="sub">Copy %s's sessions into %s</div></div></div>`,
				cls, oc, esc(from.Name), esc(to.Name), esc(from.Name), esc(to.Name)))
		}
	}
	if count == 0 {
		cards.WriteString(`<div class="empty">Add at least two managed accounts to sync between them.</div>`)
	}
	if waiting > 0 {
		word := "account"
		if waiting > 1 {
			word = "accounts"
		}
		cards.WriteString(fmt.Sprintf(
			`<div class="empty">%d %s not signed in yet, so it can't be synced. Switch to it and sign in first.</div>`,
			waiting, word))
	}
	body := `<div class="header">
  <button class="back" onclick="send('showSettings','')">‹</button>
  <div class="htext"><h1>Sync sessions</h1><p>Claude closes, then reopens where you were</p></div>
</div>` + st + `
<div class="cards">` + cards.String() + `</div>`
	return shell(body)
}

// ComputePreselect returns the folders to pre-check in the Rescan view.
func ComputePreselect(accounts []core.ScannedAccount, managed []string) map[string]bool {
	firstRun := managed == nil
	set := map[string]bool{}
	for _, m := range managed {
		set[m] = true
	}
	pre := map[string]bool{}
	for _, a := range accounts {
		// Folders awaiting sign-in count too: they are switch targets, and
		// leaving them unchecked on a first run is what hides the profile the
		// user set up precisely so they could sign in to it.
		if (a.Complete || a.SignedOut) && (firstRun || set[a.HomeFolder]) {
			pre[a.HomeFolder] = true
		}
	}
	return pre
}

// RemovedVM drives the screen shown after a removal, in either outcome.
//
// A partial failure can hand back both an ArchiveDir and an Err: the folder
// moved but a registry write did not. RegistryNote is that case: the caller
// (cmd/mcs-menubar) sets it, not Err, when the destination is non-empty, so
// this always draws the success variant underneath a folder that did in fact
// move, with the leftover complaint on the screen itself rather than in a
// status line the user may never look at again.
type RemovedVM struct {
	Folder string // for Try again after a failure
	Name   string
	Convos int
	// ArchiveDir is the base name of where it landed; empty on failure.
	ArchiveDir string
	// Err is empty on success.
	Err string
	// RegistryNote is set only alongside a non-empty ArchiveDir: the folder
	// moved but something it left behind (its display name, its managed
	// listing, ...) could not be cleared. Empty renders nothing.
	RegistryNote string
}

// RenderRemoved reports the outcome on its own screen rather than as a line at
// the top of a changed list. A removal that reports itself in one line is the
// case where the user cannot tell whether it happened, which for a
// destructive-looking action is the one thing the screen has to answer.
func RenderRemoved(vm RemovedVM) string {
	esc := html.EscapeString

	if vm.Err != "" {
		// Folder travels as data-* and is read back with dataset, never
		// interpolated into the onclick string: html.EscapeString turns an
		// apostrophe into &#39;, the HTML parser decodes it back to ' before the
		// inline JS is parsed, and a display name or folder containing one would
		// break the handler (the v0.9.1 bug).
		body := `<div class="header">
  <button class="back" onclick="send('showList','')">‹</button>
  <div class="htext"><h1>` + esc(vm.Name) + ` was not removed</h1><p>Nothing was moved</p></div>
</div>
<div class="errbox">` + esc(vm.Err) + `</div>
<div class="hint">The account is still on your list, so you can try again.</div>
<div class="footer">
  <button class="btn btn-light" onclick="send('showList','')">Back</button>
  <button class="btn btn-primary" data-folder="` + esc(vm.Folder) + `" onclick="send('removeProfile',this.dataset.folder)">Try again</button>
</div>`
		return shell(body)
	}

	// Mirrors askRemove's own zero/one/many wording in the shell script: zero
	// gets its own phrase rather than falling through to a plural that would
	// read "0 conversations", and a freshly created, never-signed-in profile is
	// not a rare case to remove.
	what := "Its conversations are untouched"
	if vm.Convos == 1 {
		what = "Its 1 conversation is untouched"
	} else if vm.Convos > 1 {
		what = fmt.Sprintf("Its %d conversations are untouched", vm.Convos)
	}
	// A registry that could not be cleared is not styled as an error: the folder
	// really did move, which is the thing this screen exists to confirm. But it
	// cannot be silent either — a display name left behind is inherited, without
	// warning, by any later account that reuses this identity, and this screen is
	// the only place that will ever say so.
	// One line per complaint. The note carries errors.Join output, whose entries
	// are separated by newlines, and HTML collapses those: two separate things
	// that could not be cleared ran together into one sentence that read as
	// neither of them.
	registryNote := ""
	if vm.RegistryNote != "" {
		var lines strings.Builder
		for _, line := range strings.Split(vm.RegistryNote, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				lines.WriteString(`<div class="noteline">` + esc(line) + `</div>`)
			}
		}
		registryNote = `<div class="hintw">` + lines.String() + `</div>`
	}
	body := `<div class="header">
  <div class="htext"><h1>` + esc(vm.Name) + ` removed</h1><p>It is off the switcher</p></div>
</div>
<div class="hint">` + esc(what) + `, in a folder called <b>` + esc(vm.ArchiveDir) + `</b> inside your archive.</div>
` + registryNote + `
<button class="sbtn" onclick="send('openArchive','')">Open archive folder</button>
<div class="footer">
  <button class="btn btn-primary" onclick="send('showList','')">Done</button>
</div>`
	return shell(body)
}

// ShortID trims a UUID to its leading 8 characters for compact display.
func ShortID(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}
