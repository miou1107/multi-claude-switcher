package main

import (
	"fmt"
	"html"
	"strings"
)

// profileVM is one row in the panel.
type profileVM struct {
	Folder  string // data dir folder name (switch target)
	Name    string // display name / email
	Team    string // "Team" | "Personal" | ""
	Current bool   // the running profile
}

// renderPanel builds the self-contained popover panel: a soft light-gradient
// header plus one clickable card per managed account (click → switch), and a
// footer with Rescan + Quit. Actions are dispatched to Go by navigating to
// mcs://… URLs, which the WKNavigationDelegate intercepts.
func renderPanel(profiles []profileVM) string {
	esc := html.EscapeString
	var cards strings.Builder
	for _, p := range profiles {
		badge := ""
		switch p.Team {
		case "Team":
			badge = `<span class="pill pill-team">🏢 Team</span>`
		case "Personal":
			badge = `<span class="pill pill-personal">Personal</span>`
		}
		if p.Current {
			cards.WriteString(fmt.Sprintf(`
      <div class="card current">
        <div class="dotcur"></div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div><div class="sub">Current account</div></div>
      </div>`, esc(p.Name), badge))
			continue
		}
		cards.WriteString(fmt.Sprintf(`
      <div class="card selectable" onclick="act('switch','%s')">
        <div class="chev">⇄</div>
        <div class="body"><div class="row1"><span class="name">%s</span>%s</div><div class="sub">Switch to this account</div></div>
      </div>`, esc(p.Folder), esc(p.Name), badge))
	}
	if len(profiles) == 0 {
		cards.WriteString(`<div class="empty">No managed accounts yet. Run Rescan to add some.</div>`)
	}

	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Multi-Claude Switcher</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html{color-scheme:light}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif;color:#241f38;
  background:linear-gradient(160deg,#efe9fb 0%,#f6eaf2 55%,#f9edf1 100%);padding:16px;-webkit-font-smoothing:antialiased;width:380px}
.header{display:flex;align-items:center;gap:11px;margin:2px 2px 14px}
.avatar{width:40px;height:40px;border-radius:12px;flex:none;background:linear-gradient(140deg,#8a74f0,#b96cee 55%,#e0607a);
  display:flex;align-items:center;justify-content:center;box-shadow:0 5px 13px rgba(124,108,240,.32)}
.avatar svg{width:22px;height:22px}
.htext h1{font-size:16px;font-weight:800;letter-spacing:-.01em}
.htext p{font-size:12px;color:#6b6580;margin-top:1px}
.cards{display:flex;flex-direction:column;gap:9px}
.card{display:flex;align-items:center;gap:12px;background:#fff;border-radius:14px;padding:12px 14px;
  box-shadow:0 3px 10px rgba(60,40,90,.06);border:2px solid transparent;transition:.13s}
.card.selectable{cursor:pointer}
.card.selectable:hover{box-shadow:0 5px 15px rgba(60,40,90,.13);border-color:#e0dcf3}
.card.current{border-color:#b7f0cd;background:#fbfffd}
.chev{width:24px;height:24px;flex:none;border-radius:8px;background:#f1eef9;color:#7c6cf0;font-size:14px;
  display:flex;align-items:center;justify-content:center}
.dotcur{width:9px;height:9px;flex:none;border-radius:50%;background:#1a8a4f;margin:0 7px 0 3px;box-shadow:0 0 0 3px #d6f5e3}
.body{flex:1;min-width:0}
.row1{display:flex;align-items:center;gap:7px}
.name{font-size:14px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sub{font-size:11.5px;color:#8b8598;margin-top:1px}
.pill{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;white-space:nowrap}
.pill-team{background:#d6f5e3;color:#1a8a4f}
.pill-personal{background:#eceaf3;color:#6b6580}
.empty{color:#8b8598;font-size:13px;text-align:center;padding:18px 8px}
.footer{display:flex;gap:9px;margin-top:14px}
.btn{flex:1;font:inherit;font-weight:700;font-size:13px;border:none;cursor:pointer;border-radius:11px;padding:10px;transition:.13s}
.btn-rescan{background:#fff;color:#514b66;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.btn-rescan:hover{background:#f6f4fb}
.btn-quit{flex:none;padding:10px 15px;background:#fff;color:#b0455f;box-shadow:0 3px 9px rgba(60,40,90,.08)}
.btn-quit:hover{background:#fdf2f5}
</style></head><body>
<div class="header">
  <div class="avatar"><svg viewBox="0 0 24 24"><circle cx="8" cy="12" r="3.2" fill="#fff"/><circle cx="16" cy="12" r="3.2" fill="#fff"/><circle cx="8" cy="12" r="1.3" fill="#7c6cf0"/><circle cx="16" cy="12" r="1.3" fill="#7c6cf0"/></svg></div>
  <div class="htext"><h1>Multi-Claude Switcher</h1><p>Switch or manage your Claude accounts</p></div>
</div>
<div class="cards">` + cards.String() + `</div>
<div class="footer">
  <button class="btn btn-rescan" onclick="act('rescan','')">⟳&nbsp; Rescan accounts…</button>
  <button class="btn btn-quit" onclick="act('quit','')">Quit</button>
</div>
<script>
  function act(a,arg){ window.mcsAct(a, arg||''); }
</script>
</body></html>`
}
