# CHANGELOG

## [Unreleased]

## [0.13.0] - 2026-08-04

### Added
- **Remove an account.** Reached from the pencil on an account row, which now
  opens an Account settings screen (renaming lives there too), with the remove
  button at the bottom, styled as destructive. The confirmation states plainly
  that the folder moves to the archive folder, openable from Settings, and
  that using the account again means signing in to it once more. Nothing is
  ever deleted. Removal is refused for an account Claude currently has open,
  so a different account can be removed without quitting Claude, and, on the
  Windows Store build, for the account occupying the shared slot, an install
  MCS has not recorded slot state for yet, and an account whose conversations
  are still queued to move into a newly added account. The button is hidden
  entirely when only one account is listed. A result screen afterwards names
  the archived folder and offers to open it, or, on failure, states that
  nothing was moved and the account is still listed.

### Fixed
- **Windows knows an account is open even when you did not open it from MCS.**
  Claude Desktop started from the Start menu, a shortcut or a login item passes
  no profile flag and runs on your default account, and on the standalone
  Windows build MCS read that as "nothing is running". Switching and syncing
  therefore closed that window without putting it back, and the removal guard
  had nothing to guard on. It is now recognised, the way it already was on
  macOS.
- **macOS ignores a second click while it is busy switching.** Picking an
  account while a switch, sync, backup, merge or removal was already running
  started a second operation on top of the first. Windows has refused that
  since it was written; macOS now does too, and shows the same "Closing
  Claude Desktop and switching" line while it works.

## [0.12.0] - 2026-08-04

### Added
- **A Debug info screen, and a way to report a problem from it.** Settings now
  shows what MCS knows about your machine: its own version, which Claude Desktop
  build you have, what each account looks like on disk, and the tail of every log
  file. "Report a problem" copies that report to your clipboard and opens a new
  issue on the project's GitHub page for you to paste it into, so you see exactly
  what is published before anything is, and you submit it yourself. Email
  addresses, account IDs, your user name and your home path are replaced with
  stable stand-ins first, and there is no way to turn that off.

### Fixed
- **There is a log file on macOS.** The menu-bar app never opened one, so
  everything it logged went nowhere and "Open log folder" showed an empty folder
  on a machine that had been running for months.

## [0.11.2] - 2026-08-04

### Fixed
- **Conversations can be synced into a Claude Team account.** They always could;
  this tool was putting them in the wrong place. Conversations are filed by
  account and by organization, and syncing rewrote only the account, leaving them
  under the organization they came from — a folder the receiving account never
  reads. The files arrived complete and invisible, which looked exactly like a
  Team account refusing an import, and was documented as one. Both halves of the
  path are now rewritten. The warnings shown before syncing into a Team account,
  and the note marking Team accounts as unable to receive one, are gone.
- **An account you had open no longer disappears when you switch or sync.**
  Closing Claude Desktop closes every account at once, but only one was ever
  reopened, so a second account you had open was taken away without a word. Every
  account that was open is now put back, except the one you switched away from.
- **Which account a switch leaves closed is no longer a guess.** With more than
  one account open, the switcher had no record of which one you were on and used
  whichever the system named first, so switching to a third account closed one of
  the two arbitrarily. The account you were last switched to is now remembered,
  and the record is ignored once that account is no longer open.
- **The account you are on is recognised even when you did not open it from the
  switcher (macOS).** Claude Desktop only carries the marker the switcher matches
  on when the switcher itself launched it. Opened from the Dock, from Spotlight,
  from a login item, or relaunched by its own updater, your main account was
  invisible: the switcher reported whichever *other* account was running as the
  one you were on, showed that account as current, and reopened that one after a
  sync. This is why a sync could leave you sitting in the wrong account.

## [0.11.1] - 2026-07-31

### Fixed
- **Switching accounts on the Windows Store build.** Every switch failed with
  `Access is denied`. Two programs keep running out of the account folder after
  Claude Desktop closes, the Chrome bridge helper and the Claude Code CLI, and
  Windows will not rename a folder a running program was loaded from. Neither was
  recognised, because the switcher looked for processes called `Claude.exe`. They
  are now found by where they run from, including the redirected `%APPDATA%\Claude`
  spelling the Store build reports, and closed before the folder is moved. The
  switcher's own parent processes are never closed, so a switch started from inside
  a Claude Code session stops with an explanation instead of terminating itself
  halfway through.
- **A switch no longer fails when Claude recreates its folder mid-swap.** A switch
  moves two folders and Claude is relaunched between them, recreating its data
  folder within seconds; the second move then landed on an existing folder, which
  Windows also refuses with a bare `Access is denied`. Anything that appears there
  is now moved aside first, and kept rather than deleted, because a sign-in
  completed in that window is real data.
- **A failed switch now says so.** The error was discarded, so a switch that had
  not happened looked exactly like one that had, and the account list only went
  strange later. The failure is shown, and when the rollback fails too the message
  names where the data is and how to put it back.
- **Switching is guarded against a second click**, the way syncing, backing up and
  merging already were. Two switches running at once raced over one folder.
- **A switch that fails in both directions no longer strands your account.** If the
  move in fails and the move back fails too, the folder left live is one Claude
  recreated, not your account. The record of which account is live now says so,
  which means your account stays listed exactly once instead of twice, and
  switching to it puts it back — before this, that switch reported success and did
  nothing, and everything afterwards worked on the empty folder while your
  conversations sat where the account list could not reach them. When even that
  record cannot be written, the message names the folder in the way, because the
  old advice to move your folder back could not be followed while something else
  was sitting in its place.
- **Closing a program that is holding the account folder can no longer hit the
  wrong one.** Programs were closed by process number, and Windows hands numbers
  out again after a program exits, so one that quit on its own in the meantime
  could take an unrelated program — and everything it had started — down with it.
  Each one is now confirmed to still be the program in that folder at the moment
  it is closed.
- **"Check for updates" updates you, on Windows.** It opened the releases page in a
  browser and did nothing else — whether or not a newer version existed — so the
  silent installer could only ever be reached by the background check that runs at
  startup and every six hours. There was no other route: the tray's right-click menu
  on Windows offers Quit alone. The button now runs the real check and installs what
  it finds, and tells you when you are already up to date instead of sending you to
  a web page to work it out yourself.

## [0.11.0] - 2026-07-30

### Added
- **A way to add an account, from the panel, on macOS and the Windows Store build.**
  The flow that saves your current account, opens a clean Claude for you to sign into
  another one, and brings that account's saved conversations across is now offered
  right on the account list, through an in-panel screen that names the new profile.
  Both hosts use the same screen, so they behave identically. (The Windows standalone
  build, whose profiles you pick by hand rather than MCS creating them, does not have
  this.)
- **Rescan offers to recover an account signed out from inside Claude Desktop.** Such
  an account used to show up only as an "Unrecognized account" you could not act on,
  even though all of its conversations were still on disk. It now appears as "Signed
  out in Claude Desktop" with a Recover button that gives it a profile again and signs
  you back in. A genuinely broken account still shows read-only, as before.
- **The account list warns when two profiles hold the same account, and merges them.**
  Both cards are marked "Duplicate" and a banner opens a merge screen that keeps the
  profile you are using, combines the conversations, and archives the other folder
  (nothing is deleted). A state that only ever causes confusion is surfaced rather than
  left for you to discover. One pair is offered at a time — each merge needs Claude
  closed.

### Changed
- **Closing Claude Desktop now always asks first.** Syncing closed Claude on a single
  click with no warning at all, while switching accounts had a confirmation the whole
  time. Both now ask, both say what happens next, and both warn that unsaved work in
  Claude is interrupted. Cancel holds the focus, so pressing Enter on a dialog you
  have not read cannot close your app. `mcs switch` asks on the command line too and
  takes `-y` to skip; with no terminal to ask on and no `-y` it refuses rather than
  assuming consent.

### Fixed
- **The sync screen worked again on macOS.** It reported every account as "not signed
  in yet" and offered no sync at all, however many accounts were signed in, so the
  screen had been unusable since 0.10.1. The macOS panel was not filling in one field
  the Windows panel did; both panels now build their account list from the same code,
  so the two cannot drift apart again.

- **Switching to a profile that is not there no longer closes Claude first.** A stale
  or mistyped folder name killed the running Claude Desktop and only then failed, and
  had the folder been created on the way it would have switched you into an empty
  profile you never asked for. The target is now checked before anything is closed.

- **An interrupted copy no longer damages a conversation.** Copies were written
  straight into the destination, which truncates it immediately, so a process that
  died partway left a truncated file behind. That was worse than a failed copy: a
  truncated file's timestamp is the moment it was written, making it newer than its
  source, and sync keeps whichever copy is newer. The next sync therefore treated
  the truncated file as the current version of that conversation and kept it. Copies
  are now staged and swapped into place, so an interruption leaves the destination
  exactly as it was.

- **Quitting during a switch or a sync no longer leaves you with no Claude.** Both
  close Claude Desktop, do their work, and reopen it. Quitting in that window killed
  the work in progress, so nothing ever reopened Claude and you were left with
  neither app running. Quit now waits for the operation to finish — it reads
  "Finishing up, then quitting…" — and leaves once Claude is back. It gives up
  waiting after 30 seconds and reopens Claude itself, so Quit is never a dead button.

- **Syncing no longer widens the permissions on your conversations.** Claude Desktop
  writes session files so only you can read them (`0600`). Staging a copy through a
  new file gave it the default instead, which on most machines means group- and
  world-readable, and every synced conversation came out that way. Copies now carry
  the source's permissions.

- **A backup taken in the same second as the previous one is no longer blended into
  it.** Snapshot folders are named to the second, and a second snapshot merged into
  the existing folder rather than replacing it, leaving conversations that had been
  deleted in between still sitting alongside the newer ones. The result matched no
  state the profile was ever in. Colliding names now get a counter.

### Changed
- **The automatic safety backup reuses the last snapshot when nothing has changed.**
  A backup is taken before every switch, sync and restore, and the panel takes one on
  every Sync click. Each was a full copy of the profile's conversations, so a heavily
  used machine accumulated dozens of near-identical snapshots (one reached 1.6 GB
  across 65 of them). MCS now compares the profile against the newest snapshot and
  reuses it when they match. The comparison covers every file in the tree, not just
  the conversations: Claude records a deleted conversation by writing a marker file
  beside them, so ignoring those would have let a deletion slip past unnoticed.
  Nothing is deleted to save space, and **Back up all accounts** still always takes a
  fresh snapshot, because you asked it to.

### Fixed
- **Sync could write outside the sessions folder, and could truncate a file it
  meant to leave alone.** The check for "does the target already have this file"
  treated every `stat` failure as "no", and the copy then truncated through
  `os.Create`. So a dangling symlink where the target's copy belonged sent the
  data to wherever the link pointed, outside the sessions directory, and an
  ordinary permission or I/O error destroyed the target's copy. Now only a
  genuine "not found" counts as absent, the check does not follow symlinks, and
  anything that is not a regular file is skipped rather than written through.

- **One unreadable file no longer stops every other conversation from syncing.**
  A per-file failure aborted the whole run and returned no report at all, so a
  single stray entry could block hundreds of healthy conversations indefinitely
  and the caller could not even say how far it got. Failures are now recorded
  per file, the run continues, and the count is surfaced in the panel and written
  to the log.

- **A failed switch no longer leaves Claude Desktop closed.** With **Auto Sync on
  switch** enabled, a backup or sync failure returned early — after Claude had
  already been terminated and before the target was launched. The user was left
  with Claude shut, holding an error, with no indication of which account they
  would land in if they reopened it by hand. The switch now always reopens the
  target; a failed sync is reported instead of stranding you. Nothing is written
  when the sync is skipped, so this is safe.

- **The panel's Sync button no longer writes into a profile Claude is running
  on, and no longer skips the backup.** It called the low-level sync directly,
  bypassing the step that closes Claude Desktop first, snapshots the target, and
  reopens the profile you were on. Writing into a live profile risks corrupting
  the session index Claude holds open, and the missing snapshot contradicted the
  README's promise that every write is preceded by one. The command-line `mcs
  sync` was already doing both correctly; the panel is now on the same path.

- **Two-way sync no longer reports clashes it went on to resolve.** **Auto Sync
  on switch** runs the union in both directions. The first direction reports a
  clash whenever the other side's copy is newer, which the second direction then
  copies across — so counting the first pass as the outcome warned about a
  problem that no longer existed by the time the switch finished. Only the files
  both directions could not settle are reported now, and they are written to the
  log, which is the only place an unattended sync can report anything.

### Changed
- **Sync says what it is about to do.** The Sync screen now reads "Claude closes,
  then reopens where you were", and the progress line says "Closing Claude
  Desktop and syncing…" instead of "Syncing…". Closing Claude was always part of
  a correct sync and the panel never mentioned it.

- **Both panels share one place that words a sync result**
  (`core/syncmessage.go`), rather than each host formatting its own copy of the
  same sentence. The wording also stopped saying "1 session(s)", and the abort
  when Claude Desktop is running but its profile cannot be identified — which
  happens to anyone who opened Claude themselves rather than through the switcher
  — now reads "Quit Claude Desktop first, then try Sync again." instead of
  internal wording.

### Documentation
- **The README now says that syncing moves the Code conversation *list*, not the
  conversations.** Claude Code keeps transcripts elsewhere and clears out old ones
  on its own schedule, so a conversation can sit in the list with nothing behind
  it. On one measured machine roughly six records in ten had no transcript left.
  That is Claude's own housekeeping, which this tool neither causes nor can undo,
  and it was previously undocumented.

## [0.10.2] - 2026-07-29

### Changed
- **The version number now shows on the main account screen, not just in
  Settings, and both read the same way.** The account list gained a small
  `v<version>` line under the Rescan/Settings buttons, and the Settings footer
  changed from `Multi-Claude Switcher <version>` to the same `v<version>` form,
  so the two panels agree. Both come from the single `core.Version` value, so a
  release bump moves them together with nothing else to edit.

### Documentation
- **Rewrote both READMEs from the reader's problem instead of the feature list.**
  The old page was written outward from the product: here are my features, let
  me introduce them. It opened with three stacked screenshots and a warning
  about what the tool *cannot* do, never said why anyone has two Claude accounts
  in the first place, and put the download link a third of the way down behind
  ~50 lines of sync caveats. The section titled "Quick Start" began with
  `go build`, ten lines after the page promised "nothing to build or compile".

  The rewrite leads with the two situations people are actually in — a work
  account and a personal one, or a limit hit on one account with quota left on
  another — then the friction today, then what changes. Sync moved out of the
  opening entirely: it is the second-order feature, it carries most of the
  caveats, and it was occupying the most expensive space on the page.

  The two READMEs are now written independently rather than one being a
  translation of the other, so each reads natively.
- **Removed the AI cadence.** An earlier draft of this rewrite had 17 em dashes
  in 166 lines and eight bullets sharing one grammatical template
  (`**bold** — explanation`). Both are gone: the English README now has zero of
  each.
- **Reworked the openings and the FAQ against a competing draft.** A second
  model's version of this README was better in specific, adoptable ways, and
  those were taken: "Why you'd want this" as a question addressed to the reader
  rather than the internal-sounding "The problem"; the two situations as scannable
  labelled bullets instead of prose; a numbered three-point answer; the
  prerequisite promoted to a callout at the top of Install; and troubleshooting
  rewritten as questions in the user's own voice ("My conversation history is a
  mess. Can I get the old one back?") rather than statements, since that is how
  people search. Its marketing intensifiers ("seamless", "perfectly preserves"),
  its empty badge links, and its `claude.ai/download` (a 301 to
  `claude.com/download`) were not adopted.
- **Fixed documentation that was wrong, not just misplaced.**
  - The README claimed MIT but the repository had **no `LICENSE` file** — added.
  - **Homebrew was never mentioned**, though CI has been maintaining the cask in
    `miou1107/homebrew-tap` on every release. The shortest macOS install existed
    only in a workflow comment; it is now the first thing offered.
  - The macOS build instructions said `go build ./cmd/mcs-tray`, but the shipped
    macOS app is built from **`./cmd/mcs-menubar`** — following the README got
    you a different, unshipped program. `docs/building.md` now spells out which
    of the four binaries each platform ships.
  - The **CLI was documented as a product it is not**: releases contain only the
    tray/menu-bar app, so no user who downloads this has `mcs`. `docs/cli.md`
    now opens by saying so, and the Automated Backup bullet no longer cites
    `mcs sync` as an ordinary way to use the app.
  - **Rescan was described as opening a review page in the browser** from a
    "Maintenance" submenu. That flow was replaced by an in-panel view, and the
    same README already said the tray's right-click menu holds only **Quit**.
  - **"Repository Structure" listed no source directory at all** — no `cmd/`,
    `core/`, `platform/` or `internal/` — and duplicated `FILELIST.md`, which is
    accurate and maintained. Removed.
- **Replaced the README screenshots, and made the replacement regenerable.** One
  of the old images showed the v0.4.0 dropdown menu, a UI removed six releases
  ago; the other exposed real account names and a private project list on a
  public page. The new `docs/assets/panel.png` is **rendered from the shipped
  panel code** by `scripts/gen-screenshot` with placeholder accounts, so it
  cannot drift from the real interface and never contains anyone's real data.
  Regeneration is one command, documented in `docs/building.md`.
- **Answered questions the README never did:** how to uninstall on either
  platform, that switching necessarily restarts Claude Desktop, that backups are
  never pruned, that the app is not affiliated with Anthropic, and that it never
  handles credentials.
- New pages: `docs/building.md`, `docs/cli.md`, `docs/team-accounts.md`,
  `docs/how-it-works.md`.

## [0.10.1] - 2026-07-28

### Internal
- **Replaced real personal data in tests and design docs with placeholders.**
  Test fixtures and the design/plan documents had been written from live
  on-device data, so a real email address, real Claude account UUIDs, a real
  name and employer, and local home directories from both machines were
  committed to a public repo. All are now obviously fake
  (`first@example.com`, `11111111-1111-…`, `C:\Users\Example\`,
  `/Users/example/`), with every assertion kept intact. No behaviour change.
  Note that these values remain in the git history.

### Changed
- **Windows: updates install themselves, as on macOS.** The periodic check
  already ran on Windows, but finding a newer version only produced a
  notification and, on a manual check, a browser tab — the user then had to
  download the installer and run it. The updater now downloads the release's
  `setup.exe`, checks it really is an executable (a captive portal answering
  `200` with an HTML page would otherwise be handed to `CreateProcess`), runs it
  with `/VERYSILENT /SUPPRESSMSGBOXES /NOCANCEL /NORESTART`, and quits so the
  installer can replace the running binary. The install is per-user, so there is
  no UAC prompt, and a file fetched by the app carries no Mark-of-the-Web, so
  SmartScreen does not intervene either — unlike a browser download. The
  relaunch is the installer's own `[Run]` entry, which lost its `skipifsilent`
  flag for exactly this reason; `CloseApplications=yes` is the backstop for a
  hand-run installer, and `RestartApplications=no` keeps Restart Manager from
  starting the app a second time. macOS is unchanged: it ships a bare binary in
  a zip, so it still updates by atomically renaming its own executable —
  Windows cannot, because it locks a running image.
- **Windows: the panel opens instantly.** It used to be a process started on
  every click, so each open paid for a fresh process, a fresh WebView2
  environment and a fresh page load: 1.5–2 s before anything appeared. The
  panel process is now started once, with the tray, and kept alive parked
  off-screen; a click moves a window that already exists. Measured 1.5–2 s →
  **under 30 ms**. Dismissing the panel parks it again instead of exiting, the
  tray restarts it if it ever dies, and it exits with the tray rather than
  being orphaned. Cost: ~314 MB resident (mostly WebView2's own processes) for
  the lifetime of the tray. See
  `docs/superpowers/specs/2026-07-28-windows-warm-panel-design.md`.
- **Windows: one click on the tray icon opens the panel, as on macOS.** v0.10.0
  shipped the panel behind a context-menu item, so opening it took two clicks
  (icon → **Show panel**). The tray now switches from the unmaintained
  `github.com/getlantern/systray` to its maintained successor
  `fyne.io/systray`, whose `SetOnTapped` hook gives Windows a real left-click
  callback. Clicking the icon again while the panel is open closes it, matching
  the macOS popover's toggle.
- **Windows: the tray's right-click menu is down to a single Quit.** **Show
  panel** and **About** are gone; the panel is the UI and already shows the
  version in **Settings**. Quit stays on the right-click menu on purpose: if the
  WebView2 Runtime is missing the panel cannot open, and without this item the
  app could only be stopped from Task Manager. macOS has no tray menu at all.

### Fixed
- **Rescan no longer hides a profile that is waiting to be signed in to**
  (both platforms). Sign-in is per profile folder and permanent, so a second
  account needs one sign-in in its own folder and then switching to it just
  works. But a folder in that state has no account and no sessions, and Rescan
  dropped it as junk: a user who had set up the second profile saw "one
  account" and no hint of what was missing. Such folders are now listed and
  selectable, with the next step spelled out on the card. Directories that
  merely start with "Claude" but are not profiles at all (the Claude Code CLI
  keeps one beside them) are still excluded, now by looking for a `config.json`
  rather than by having account data.
- **Windows: signing in to a switched-to profile now lands in that profile.**
  Sign-in finishes in the browser, which hands the result back through the
  `claude://` scheme; Windows resolves that with a registered command line that
  carries no `--user-data-dir`, so the callback opened the *default* profile and
  the new account was written there. The user appeared to be thrown back to the
  account they had switched away from. Switching to a profile that has no
  account now holds the `claude://` handler on that profile until the sign-in
  lands (or 10 minutes pass), then restores Claude Desktop's own registration.
  The hold is required rather than a single write: Claude Desktop re-registers
  the handler itself about 825 ms after starting, so anything written before
  launch is gone before the sign-in screen appears. Profiles that already have
  an account are never touched. Standalone build only; the Store build swaps
  profile folders and was never affected. See
  `docs/superpowers/specs/2026-07-28-windows-signin-callback-design.md`.
- **Sync no longer offers directions that cannot work** (both platforms).
  Sessions are stored per account, so a profile with nobody signed in has no
  bucket to read from or write to. Those directions were still listed and
  failed on click with the missing config key quoted back at the user. They are
  now omitted, with a line saying why, and the account list marks such a
  profile "Not signed in yet. Switch here, then sign in." rather than looking
  identical to a ready account. If a sync is attempted anyway (signed out
  between opening the view and clicking), the error now names the account and
  the fix instead of the config field.
- **Windows: the panel opens next to the tray icon instead of always in the
  bottom-right of the primary display.** The tray now passes the click position
  to the panel, which places itself inside the work area of whichever monitor
  was clicked. Fixes a panel that appeared on the wrong monitor, or over/under
  the taskbar, for taskbars that are not at the bottom of the primary display.
- **Windows: the panel no longer appears in the top-left corner for a moment
  before jumping to the tray.** Its window is created by go-webview2, which
  shows it at the system default position and only then spends a few hundred
  milliseconds embedding the browser. The window is now parked off-screen from
  the moment it exists and moved into view only once it is styled, sized and
  filled. It is parked rather than hidden on purpose: WebView2 stops rendering
  while its host window is hidden and does not resume when the window returns,
  which produces a correctly placed but completely blank panel.
- **Windows: the frame removal is applied with `SWP_FRAMECHANGED`.** Stripping
  the caption and borders without it left Windows using stale frame metrics.
- **Windows: no stray taskbar button while the panel starts.** go-webview2
  creates a plain `WS_OVERLAPPEDWINDOW`, which earns a taskbar button, and the
  button stayed for the ~750 ms the browser took to embed before the styling
  step removed it again. `WS_EX_TOOLWINDOW` is now applied the moment the
  window appears, before the shell gets a chance to add the button.
- **Windows: the panel no longer dismisses itself before it is ever seen.**
  Opening on a single tray click exposed a race that the old two-click route
  did not: the shell owns the foreground immediately after a tray click, so the
  panel's `SetForegroundWindow` was refused and the resulting deactivation was
  read as an outside click. The tray now passes the foreground right on with
  `AllowSetForegroundWindow`, the panel takes the foreground through an
  `AttachThreadInput` handover (which succeeded every time in testing, where
  the bare request succeeded about half the time), and it only arms its
  outside-click dismissal once it has confirmed it reached the foreground.
- **Windows: the panel logs where its window ended up** (position, visibility,
  focus, activation transitions). It is a windowless GUI process, so a panel
  that starts but never appears previously left nothing in the log to go on.

## [0.10.0] - 2026-07-28

### Changed
- **Windows tray now shows the same panel UI as macOS.** Previously the Windows
  tray was a plain text context menu (Profile submenus / Sync / Settings /
  Maintenance / About / Quit). It is now a minimal launcher — right-click
  shows only **Show panel**, **About**, **Quit** — and clicking **Show panel**
  opens a WebView2 popup with the same styled card list, plan badges,
  confirmation modal before switching, and in-panel Rescan / Sync / Rename /
  Settings that macOS has had since v0.9.0. The account list, switch action,
  and every settings toggle live in the panel now, not the tray menu.

### Added
- The panel HTML/CSS/JS renderer moved into a shared `internal/panelui`
  package. macOS (`cmd/mcs-menubar`, NSPopover + WKWebView) and Windows
  (`cmd/mcs-tray --panel`, jchv/go-webview2) both consume the same output, so
  the two platforms will stay in lockstep as the panel evolves.
- Escape now hides the panel on both platforms (mac closes the popover,
  Windows exits the panel process). The next tray click reopens it.

### Requires
- **Windows:** WebView2 Runtime. Preinstalled on Windows 11 and Windows 10
  21H2+ via Windows Update; on older systems the panel shows a native dialog
  with a one-click link to Microsoft's install page.

## [0.9.1] - 2026-07-28

### Changed
- **macOS panel: switching an account now asks for confirmation.** Clicking a
  non-current account in the menu-bar panel used to switch immediately, which
  would quit and relaunch Claude mid-work. It now opens a confirmation modal
  that names the target account and warns that in-progress work will be
  interrupted. Cancel with **Esc**, confirm with **Enter**.

## [0.9.0] - 2026-07-25

### Added
- **macOS: a native menu-bar panel replaces the plain dropdown menu.** Clicking the
  menu-bar icon now opens a styled NSPopover (direct CGO Objective-C: NSStatusItem +
  NSPopover + WKWebView, `cmd/mcs-menubar`) instead of a text menu. Everything lives in
  the one popover — no separate windows: an account list that shows each account's
  **subscription plan** (Max 20×, Max 5×, Max, Pro, Free, or 🏢 Team, via
  `core.DetectPlan`) and switches on click; an in-panel **Rescan** picker; **Sync**
  directions; **Rename**; and a **Settings** view (Auto Sync, Start at Login, Backup,
  Open log/backup folder, Check for updates) with background self-update. The Windows
  build keeps the systray tray (`cmd/mcs-tray`); the menu-bar panel is macOS-only.

### Changed
- Rescan accounts now opens a native window (a WKWebView-backed picker shipped as a sibling `mcs-picker` helper) with a real card list — checkboxes, Team badges, and greyed-out unmanageable accounts — replacing the plain-text dialog that was unreadable with many columns.

## [0.8.0] - 2026-07-24

### Added
- Tray now detects **Team** accounts (from the cached organization list) and tags them `🏢 Team` in the profile menu. Actions that would import Code sessions *into* a Team account — enabling Auto Sync, or a manual sync direction targeting a Team account — now warn that import is a no-op for Team accounts (they are export-only). Detection is best-effort; unrecognized accounts are left untagged rather than mislabeled.
- Rescan accounts: scan the machine for Claude accounts, review each (UUID, completeness, email, Team, conversations, last-updated), and pick which to manage. Incomplete/ghost accounts (orphaned Code sessions with no login) are shown read-only as "Invalid account data".

### Changed
- First-run menu now shows accounts with an active login (and managed profiles); a logged-out profile folder no longer appears until you run Rescan accounts.

## [0.7.9] - 2026-07-23

### Fixed
- **Tray logging was silently dropped on the GUI build.** `SetupLogging` used
  `io.MultiWriter(os.Stderr, f)`, but a `-H=windowsgui` build has no valid stderr,
  and MultiWriter aborts on the first writer's error — so nothing reached the log
  file since v0.7.5. The file is now written first and stderr errors are swallowed
  (`core/logging.go`).
- **Windows dialogs could open hidden or behind other windows.** The window-hiding
  flag also hid the WinForms dialogs, and a background tray's dialogs are not
  foreground; both are fixed (CREATE_NO_WINDOW only, plus TopMost/owner on every
  dialog) so About / Rename / Sync / new-profile prompts reliably appear in front
  (`cmd/mcs-tray/hidewindow_windows.go`, `cmd/mcs-tray/dialog_windows.go`).
- **Windows Store profile swap was fragile.** The rename that swaps the live data
  directory could fail while Claude still held its files open; the retry window is
  now ~20s, each step is logged, a failed swap cleans up and reports a clear "quit
  Claude fully" message, and no data is ever deleted (`platform/windows_msix.go`).
- **Two tray tooltips said "in Finder" (a macOS term) on every OS.** They now name
  the platform's file manager — "File Explorer" on Windows, "Finder" on macOS
  (`cmd/mcs-tray/main.go`, `cmd/mcs-tray/dialog_*.go`).

### Added
- **Windows Store build: bring the second account's saved sessions over
  automatically.** After you create your other account ("New account profile…")
  and sign into it, that account's previously saved Code sessions are copied into
  its new profile (`cmd/mcs-tray/profiles_windows.go`, `platform/windows_msix.go`).

### Documentation
- **Documented a hard limitation: Claude Team accounts are export-only.** Session
  sync can export a Team account's Code sessions OUT (Team → personal) but cannot
  import INTO a Team account (anything → Team). A Team account builds its Code
  sidebar from a server API (`sessions_api_list_sessions`, scoped to account +
  organization), so session files copied into its local folder are ignored and
  never appear, even after a clean restart or full cache wipe. Verified
  2026-07-23 on a live Team account; both READMEs now carry a top-of-page
  warning, and `docs/superpowers/specs/2026-07-22-probe-results.md` records the
  correction (the earlier "folder copy always surfaces sessions" premise was a
  false positive that only tested restoring an account's own sessions).

### Changed
- **Release CI now bumps the Homebrew tap automatically.** After the macOS build
  publishes the `_macos.zip`, a new `update-homebrew-tap` job in `release.yml`
  writes the new version and its SHA256 into
  `miou1107/homebrew-tap/Casks/multi-claude-switcher.rb` and pushes the change,
  so `brew install --cask miou1107/tap/multi-claude-switcher` tracks each
  release without a manual step. Requires a `HOMEBREW_TAP_TOKEN` repository
  secret (fine-grained PAT with Contents:read/write on the tap repo); the job
  soft-fails if the token is missing, leaving the mac/windows releases
  themselves unaffected.

## [0.7.8] - 2026-07-23

### Added
- **Windows: support the Microsoft Store / MSIX build of Claude Desktop.** The
  Store build can't be launched with a custom `--user-data-dir`, so on it MCS
  switches accounts by swapping the single live data directory in place: the
  active profile sits in `…\LocalCache\Roaming\Claude`, inactive ones are parked
  under `…\Roaming\.mcs-profiles\<name>`, and a switch renames them and relaunches
  the packaged app via its AppUserModelID. All moves are reversible same-volume
  renames (no data is deleted); a failed switch rolls back. A new **"New account
  profile…"** tray item (Store build only) saves the current account and opens a
  fresh Claude to sign into another one. The standalone build is unaffected and
  still wins when both are installed. See
  `docs/superpowers/specs/2026-07-23-windows-msix-support-design.md`,
  `platform/windows_msix.go`, `platform/windows.go`,
  `cmd/mcs-tray/profiles_windows.go`.
- **macOS: ad-hoc sign the app bundle when packaging.** `scripts/package-app.sh`
  now runs `codesign --sign -` on the `.app` before zipping. This needs no Apple
  Developer account and does not notarize the app, so a browser-downloaded copy
  still needs a one-time Gatekeeper bypass on first launch. What it buys: one
  clean whole-bundle signature with a stable identity after the universal binary
  is assembled, which keeps the self-updater's in-place binary swap
  codesign-valid. The README install steps now also cover clearing Gatekeeper on
  macOS 15 (System Settings → Privacy & Security), where the older right-click →
  Open path no longer appears.

### Changed
- **Each profile is now its own submenu.** A profile in the tray menu used to be
  a single click-to-switch item, with renaming tucked under Settings. Each
  account is now a submenu with **Switch to this profile** and **Rename…**, so an
  account's actions live together and renaming targets that account directly (no
  more "which profile?" picker). Switching is therefore one step deeper (open the
  account's submenu, then Switch). The "Rename a Profile…" entry has been removed
  from Settings. `cmd/mcs-tray/main.go`.

## [0.7.7] - 2026-07-23

### Fixed
- **Windows: PowerShell windows flashed on screen periodically.** The tray is a
  GUI process with no console of its own, so every console helper it spawned —
  the 4-second running-profile poll's `powershell`, plus `taskkill` / `tasklist`
  / `reg` — popped its own black window. Each is now launched with
  `CREATE_NO_WINDOW` (`platform/hidewindow_windows.go`,
  `core/hidewindow_windows.go`, `cmd/mcs-tray/hidewindow_windows.go`).
- **Windows: the app showed a generic Start Menu / taskbar / Explorer icon.**
  `mcs-tray.exe` carried no icon resource (`SetIcon` only themes the live tray
  glyph, not the file). A Windows icon resource generated from `icon.ico` is now
  compiled into the executable (`cmd/mcs-tray/rsrc_windows_amd64.syso`).

### Changed
- **Windows releases ship only the installer.** The `_windows.zip` is dropped, so
  `Multi-Claude-Switcher_<version>_windows_setup.exe` is the single Windows
  download. When a newer version is released the app notifies you, and a manual
  "Check for Updates" opens the download page; running the new installer upgrades
  in place. macOS keeps its silent binary-swap self-update. The self-updater is
  split into `cmd/mcs-tray/update_install_{nonwindows,windows}.go`
  (`cmd/mcs-tray/update.go`, `.github/workflows/release.yml`).

## [0.7.6] - 2026-07-23

### Added
- **Windows installer.** Releases now include
  `Multi-Claude-Switcher_<version>_windows_setup.exe`, a per-user Inno Setup
  installer (no administrator prompt) that installs the tray app, adds a Start
  Menu shortcut, and registers an uninstaller in Add/Remove Programs
  (`packaging/windows-setup.iss`, `.github/workflows/release.yml`). The
  `_windows.zip` is retained for the in-app self-updater.

## [0.7.5] - 2026-07-23

### Fixed
- **Windows tray app opened a console window and stayed attached to it.** It is
  now built with `-H=windowsgui`, so it runs from the tray with no console window
  (`.github/workflows/release.yml`).
- **Windows "Check for Updates" left a stray tray icon and showed no message.**
  The NotifyIcon-balloon approach is replaced with a proper Windows toast
  notification, which adds no extra tray icon (`cmd/mcs-tray/dialog_windows.go`
  `notify`).

### Changed
- **The Windows zip now contains only `mcs-tray.exe`.** The `mcs.exe` CLI is no
  longer bundled, matching the macOS release which ships just the app.

## [0.7.4] - 2026-07-23

### Fixed
- **Windows tray icon failed to load** ("Unable to set icon"). Startup used
  `SetTemplateIcon` with a macOS template PNG, which systray rejects on Windows.
  The icon is now set per-OS (`setTrayIcon`): a template PNG on macOS, the
  multi-resolution `icon.ico` via `SetIcon` on Windows
  (`cmd/mcs-tray/trayicon_{darwin,windows,other}.go`).

### Documentation
- Added a **Traditional Chinese README** (`README.zh-TW.md`) and an
  `English | 繁體中文` language switcher at the top of both READMEs.

## [0.7.3] - 2026-07-23

### Added
- **Windows support (in progress).** The platform layer, start-at-login, single-
  instance guard, self-update, and tray dialogs now have Windows implementations
  behind build tags, and a `windows-latest` CI job publishes a
  `Multi-Claude-Switcher_<version>_windows.zip`. Switching targets the
  **standalone** Claude Desktop build (launched with `--user-data-dir`); the
  Microsoft Store / MSIX build is detected but not yet supported for launching.
  - `platform/windows.go` — process detection (`Win32_Process`), profile
    discovery, terminate-by-PID (never the identically named Claude Code CLI),
    and standalone-exe launch.
  - `core/loginitem_windows.go` — start-at-login via the `HKCU\...\Run` key.
  - `cmd/mcs-tray/instance_windows.go` — single-instance guard via `tasklist`.
  - `cmd/mcs-tray/dialog_windows.go` — tray dialogs / notifications via PowerShell.
  - `cmd/mcs-tray/update_platform_windows.go` — self-update: `_windows.zip` asset,
    pure-Go unzip, `.exe` relaunch.

### Fixed
- `core/backup_test.go` now induces a staging-write failure in an OS-appropriate
  way (an `icacls` deny ACE on Windows, `chmod` on Unix), so the
  restore-preserves-target test passes on Windows instead of relying on POSIX
  permission bits.

## [0.7.2] - 2026-07-23

### Fixed
- **Menu-bar icon was squished.** systray forces the tray image to a 16x16
  square (`[image setSize:NSMakeSize(16, 16)]`), so the 0.7.1 template — a
  non-square 69x44 — got compressed horizontally and the eyes looked distorted.
  The template now renders on a square canvas (eyes centered with vertical
  padding), so it displays at the correct aspect
  (`scripts/gen-icons/main.go` `renderTemplate`, `cmd/mcs-tray/assets/icon.png`).

## [0.7.1] - 2026-07-23

### Changed
- **New app icon — a pair of eyes** (left large, right small, each with a
  pupil), replacing the generic swap-arrows glyph. Ships as a color app icon,
  a black menu-bar template that macOS recolors for light/dark, a
  multi-resolution Windows `.ico`, and a 512px doc image. All are generated
  from geometry by `scripts/gen-icons/main.go` (`go run scripts/gen-icons/main.go`),
  so the source of truth is code, not binaries
  (`cmd/mcs-tray/assets/{appicon-1024.png,icon.png,icon.ico}`,
  `docs/assets/icon.png`).

## [0.7.0] - 2026-07-22

### Added
- **Manual "Sync sessions" tray submenu:** copy one account's Code sessions
  into another **without switching accounts** — it closes Claude Desktop, backs
  up the target, syncs (re-bucketed under the target account), and reopens the
  account you were already on (`core/align.go` `Switcher.ManualAlign`,
  `cmd/mcs-tray/main.go`).
- **"Auto Sync on Switch" toggle (default OFF):** when on, every switch
  bidirectionally unions both accounts' Code sessions so they converge to the
  same history; safe because both profiles are closed during the switch window.
  Enabling shows a one-time warning (with an "Enable, don't ask again" option),
  since it merges one account's conversations into the other. The toggle sits at
  the top of the **Sync sessions** submenu, and while it is on the manual
  directions below it are disabled (redundant)
  (`core/settings.go`, `core/sync.go` `SyncBidirectional`,
  `cmd/mcs-tray/autosync.go`).
- **Single-instance guard:** launching a second tray while one is already running
  now shows an "already running" notice and quietly exits, so the menu bar never
  gets duplicate icons/updaters. The self-update relaunch is exempt
  (`cmd/mcs-tray/instance.go`).

### Changed
- **Switching no longer auto-syncs by default.** Previously every switch ran a
  one-way session sync; now a switch moves **no** session data unless
  "Auto Sync on Switch" is enabled. This makes cross-account conversation
  merging an explicit opt-in (`core/switch.go`).
- **Tidier tray menu:** the growing action list is grouped into **Settings** and
  **Maintenance** submenus, the version moved into a new **About** item, and only
  the frequent actions (switch, Sync sessions) stay at the top level
  (`cmd/mcs-tray/main.go`).

### Notes
- Scope is Code sessions (`claude-code-sessions`) only. Agent Mode / Cowork
  sessions (`local-agent-mode-sessions`) are not synced; that is a separate,
  display-verification-gated follow-up. Regular chat is server-side per account
  and cannot be synced locally.

## [0.6.1] - 2026-07-22

### Changed
- **The `.app` is now the only published download.** Releases no longer attach
  the raw `mcs` / `mcs-tray` binaries or the raw `_macos-universal.zip`; the sole
  asset is `Multi-Claude-Switcher_<version>_macos.zip` (the ready-to-run app), so
  there's no confusing "which file do I download" (`.github/workflows/release.yml`,
  README).
- **Self-update now sources the `.app` zip** instead of a standalone binary: it
  downloads the release zip, extracts the tray executable from
  `…/Contents/MacOS/mcs-tray` (via `ditto`), and atomically swaps that in
  (`cmd/mcs-tray/update.go`, new `findAppZip` / `findTrayBinary` / `copyExecutable`).
  Only the executable is replaced, not the whole bundle, so `Info.plist` / icon
  changes ship with a fresh install rather than a self-update.

### Upgrade note
- **Any install older than 0.6.1** (0.5.0 or 0.6.0) cannot auto-update to 0.6.1:
  their updater looks for the now-removed `mcs-tray-macos-universal` asset.
  Download the 0.6.1 `.app` once manually; 0.6.1+ self-updates normally from the
  zip thereafter.

## [0.6.0] - 2026-07-22

### Added
- **Automatic updates** (`core/update.go`, `cmd/mcs-tray/update.go`): the tray
  checks GitHub Releases on startup and every 6 hours, and when a newer version
  is available it downloads the universal binary, strips the download quarantine,
  atomically swaps it in for the running executable (with rollback on failure),
  and relaunches. New tray menu item **Check for Updates…** for a manual check.
  Verified end-to-end: a binary built as v0.4.9 self-updated to the real v0.5.0
  release and its hash matched the published asset. (Updates are trusted via
  HTTPS to the project's own GitHub Releases; per-binary checksum/signature
  verification is a planned follow-up.)
- **Double-clickable macOS `.app` bundle**: the tray now ships as
  `Multi-Claude Switcher.app` — a menu-bar-only agent (`LSUIElement`, no Dock
  icon) with a color app icon. Built by `scripts/package-app.sh` locally and by
  the release workflow (packaged into `Multi-Claude-Switcher_<ver>_macos.zip` via
  `ditto`). The app is unsigned (no Apple Developer account), so the first launch
  is a one-time **right-click → Open**; no Terminal required
  (`packaging/Info.plist.template`, `cmd/mcs-tray/assets/appicon-1024.png`).
- **Start at Login** (`core/loginitem.go`): a new checkable tray item installs or
  removes a per-user LaunchAgent
  (`~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`) so the app
  launches automatically at login. Plist writes are atomic. Enabling/disabling
  only writes/removes the plist and takes effect at the next login — it does not
  `launchctl load`/`unload` the job at runtime, which would otherwise spawn a
  duplicate instance on enable or SIGTERM the running app on disable.

### Changed
- **Self-update is bundle-aware**: when running inside a `.app`, the post-update
  relaunch goes through LaunchServices (`open -n <bundle>`) instead of exec'ing
  the raw binary, so the `LSUIElement` menu-bar-agent treatment is preserved (no
  transient Dock icon). Bare-binary runs are unchanged
  (`cmd/mcs-tray/update.go`, new `isInsideAppBundle`).

### Documentation
- **README Download section**: leads with the `.app` (double-click, first-launch
  right-click → Open) and keeps the raw binary / CLI as advanced options, with
  stable `releases/latest/download/…` links. Refreshed two stale notes: the
  resolved cross-account "known limitation" now explains how account-aware sync
  stays correct, and the tray description reflects the icon / active marker /
  auto-update instead of the old `☁️ Claude` text.
- **Design spec** `docs/superpowers/specs/2026-07-22-macos-app-bundle-design.md`.

## [0.5.0] - 2026-07-22

### Build / CI
- **GitHub Release automation** (`.github/workflows/release.yml`): pushing a
  `v*` tag builds universal (arm64 + Intel) macOS binaries with the version
  baked in via `-ldflags`, packages a zip + checksum, and publishes a GitHub
  Release with the raw binaries attached (the download source for the upcoming
  auto-updater). `core.Version` is now a `var` so the tag can be injected.

### Added
- **Active-profile marker in the tray**: the profile currently in use is shown
  with a checkmark and "(current)", updated after a switch and by a background
  poller so it stays correct even when the profile is changed outside the tray
  (`cmd/mcs-tray/main.go`, `platform.DetectRunningProfile`).
- **Custom profile display names**: rename profiles to friendlier labels via the
  new tray item **Rename a Profile…** (native dialogs); stored in
  `~/.multi-claude-switcher/names.json` (`core/names.go`). Names are used in the
  menu, the switch confirmation, and the active marker.
- **Menu bar icon** for the tray instead of the literal text "Claude": a
  swap-arrows template glyph that macOS recolors for light/dark menu bars
  (`cmd/mcs-tray/assets/icon.png`, embedded via `go:embed`).
- **Persistent logging** (`core/logging.go`): the tray and mutating CLI commands
  now append to `~/.multi-claude-switcher/logs/<component>.log` (plus stderr), so
  a background/auto-started tray leaves a durable trail. New tray menu item
  **Open Log Folder**.

### Fixed (correctness)
- **Sync is now account-aware — the switch's core actually works cross-account.**
  `SyncSessions` previously copied buckets at their verbatim path, so switching
  between two different accounts (a) dropped sessions under the *source* account's
  bucket name where the target app never looks (silent no-op), and (b) dragged
  foreign/orphaned buckets into the target, re-polluting it. It now reads the
  source profile's own account bucket and re-homes those sessions under the
  **target** profile's `lastKnownAccountUuid` bucket, copying only that one bucket
  (`core/sync.go`, new `platform.GetProfileAccountUUID`). `SyncReport` now reports
  `SourceAccount` / `TargetAccount`, surfaced by `mcs sync` and the Safe Switch log.
  Safe Switch gracefully **skips sync but still launches** when a profile has no
  logged-in account yet, so `switch` can still open a fresh profile to log into.
  Tests: `TestSyncRebucketsIntoTargetAccount`, `TestSyncErrorsWhenNotLoggedIn`,
  `TestSyncNoOpWhenSourceBucketMissing`, `TestSafeSwitchLaunchesWhenTargetNotLoggedIn`.

### Documentation
- **Phase 0 findings corrected with live-machine evidence** (`docs/superpowers/specs/2026-07-22-probe-results.md`):
  - The Code tab enumerates sessions **only** from `claude-code-sessions/<lastKnownAccountUuid>/`; copying a session bucket under any other name is a silent failure (files on disk, empty sidebar). Sync MUST re-bucket under the *target* profile's account UUID. Confirmed by a real natural experiment on two live profiles.
  - Falsified an earlier hypothesis that `config.json` `dxt:allowlist*` / Local Storage leveldb drives the list; the account-UUID bucket name is the whole gate.
  - Added a Config / Preferences sync analysis: config files are not monolithic (global prefs = whitelist-copy, per-account maps = merge-by-key, identity/auth = never sync). Bypass Permissions is a per-account opt-in in `claude_desktop_config.json`.
  - Closes the 0.4.0 "Known limitation": a source-only `<AccountUUID>` bucket **does** surface in the target app once copied under the target's account UUID (verified on-device by restoring a personal-account bucket into the personal profile).
- **Design spec** (`...-multi-claude-account-sync-design.md`): added the bucket-naming invariant to the Safe Switch steps and refined the shared/isolated boundary to field-level config sync.

## [0.4.0] - 2026-07-22

### Fixed (safety hardening)
- **Safe Switch never overwrites without a backup**: if the target profile has
  existing sessions and the pre-switch backup fails, the switch now aborts
  instead of proceeding to overwrite (`core/switch.go`).
- **Sync no longer silently destroys data**: `SyncSessions` compares content and
  only overwrites when the source is strictly newer; when the target holds a
  different, newer version it is left untouched and reported as a conflict
  (`core/sync.go`, `SyncReport.ConflictCount` / `Conflicts`).
- **Termination is verified**: `TerminateApp` now returns an error if a Claude
  Desktop process is still running after force kill, so we never sync into a
  live-writing profile (`platform/darwin.go`).
- **Atomic restore**: `RestoreBackup` stages into a temp dir and swaps in only on
  success, so a mid-restore failure no longer half-destroys the target
  (`core/backup.go`).
- **Restore is reversible**: `RestoreBackup` now snapshots the current target
  before overwriting it, and aborts if that snapshot fails. Restoring the wrong
  backup is no longer a one-way loss of whatever the target held (`core/backup.go`).
- **Restore refuses to run while Claude Desktop is open**: `mcs restore`
  overwrites the live session index, so it now guards on `IsAppRunning` like
  `mcs sync` (`cmd/mcs/main.go`).
- **Standalone `mcs sync` is now safe**: refuses to run while Claude Desktop is
  open (avoids writing into a live-writing profile), and aborts on a genuine
  backup failure instead of silently overwriting (`cmd/mcs/main.go`).
- **`DetectRunningProfile` handles profile paths that contain spaces**: the
  default profile path is `.../Application Support/Claude`, and `ps` renders
  args space-joined without quoting; detection now matches against known profile
  paths with an argument boundary instead of splitting on spaces
  (`platform/darwin.go`). Prevents the tray from picking a truncated source path
  and failing the switch after closing Claude.
- **Copies preserve source modification time** (`os.Chtimes`), so sync's
  mtime-based conflict detection stays meaningful across repeated runs
  (`core/backup.go`).

### Changed
- **Single version source of truth** (`core/version.go`); CLI and tray import it
  (previously 0.1.0 / 0.2.0 / 0.3.0 disagreed across files).
- **Tray picks the running profile as the sync source** via the new
  `DetectRunningProfile` platform method, instead of an arbitrary other profile.
- **Tray confirms before switching**: clicking a profile now shows a native
  confirmation dialog (osascript), since the switch closes Claude Desktop; a
  mis-click no longer silently kills a running session. Switch failures surface
  as a macOS notification (`cmd/mcs-tray/main.go`).

### Known limitation
- Cross-account sync only reliably surfaces buckets that already exist on both
  profiles. Whether a source-only `<AccountUUID>` bucket appears in the target
  app is unverified on-device (Phase 0 probe open item) and needs a real
  end-to-end test. **(Closed in [Unreleased] — verified on-device.)**

## [0.3.0] - 2026-07-22

### Added
- **macOS System Tray GUI (`mcs-tray`)**:
  - `cmd/mcs-tray/main.go`: Menu bar quick switcher using `github.com/getlantern/systray`.
  - Dynamic profile listing and 1-click Safe Switch trigger from macOS menu bar.
  - Quick backup trigger and Finder folder shortcut.

## [0.2.0] - 2026-07-22

### Added
- **Go Core & CLI Engine (`mcs`)**:
  - `platform/`: Platform abstraction interface (`platform.go`) and macOS Darwin implementation (`darwin.go`) for process control (`pkill`), profile discovery, and `--user-data-dir` launch.
  - `core/`: Backup manager (`backup.go`), session index sync engine (`sync.go`), and Safe Switch controller (`switch.go`).
  - `cmd/mcs/main.go`: Command-line tool supporting `mcs status`, `mcs sync`, `mcs switch`, `mcs backup`, and `mcs restore`.
  - Unit test suite: `core/backup_test.go` and `core/sync_test.go` (100% passing).

## [0.1.0] - 2026-07-22

### Added
- **Phase 0 Probe Suite**: `scripts/probe/probe_runner.py` for profile status inspection, session backup, and `--user-data-dir` launch validation.
- **Probe Findings Report**: `docs/superpowers/specs/2026-07-22-probe-results.md` confirming `--user-data-dir` support and Safe Switch mode feasibility on macOS.
- **Implementation Plan**: `docs/plans/2026-07-22-phase-0-probe.md` outlining probe tasks and safety verifications.
- Core documentation: `README.md` and `FILELIST.md`.
