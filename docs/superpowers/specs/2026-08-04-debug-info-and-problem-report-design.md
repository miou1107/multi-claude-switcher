# Debug info and problem report

A panel screen that shows exactly what MCS knows about this machine, and turns
that into a GitHub issue the user files themselves.

## Why

Every bug report so far has arrived without the two facts that decide the
answer: which Claude Desktop build is installed, and what the profiles actually
look like on disk. Both are already on the user's machine and neither is
reachable from the UI. The macOS panel cannot even show a log, because the
menu-bar process never opens one.

## What it is not

**Nothing is uploaded.** MCS copies the report to the clipboard and opens a
prefilled GitHub issue page; the user pastes, reviews and submits under their own
account. There is no endpoint, no credential in the binary, and no telemetry.

Shipping an upload would mean shipping a write credential in an open-source
binary, or standing up a service to hold one. Neither is worth it for a tool
whose bug reports arrive at a rate of a handful a month, and the clipboard route
has a property an upload cannot match: the user sees the exact bytes before
anything leaves.

## Entry point

Settings screen, a new `.sbtn` row under "Open log folder": **Debug info**.

## The screen

One view, `debug`. Top to bottom:

1. Back button to Settings.
2. A green notice: *"Email addresses, account IDs and your user name are removed
   before anything leaves this screen."*
3. The report, monospace, in a scrolling box.
4. `What went wrong? (optional)` — a multi-line text box.
5. `Copy` and `Report a problem`.

No toggles. The log is always included and masking is never disabled — what is
on screen is what gets copied, with no second version of the truth.

`Report a problem` opens the existing confirm modal:

> **Open a GitHub issue?**
> The report above and your comment are copied to your clipboard, and your
> browser opens a new issue on the MCS repository. Paste it there and you can
> still edit it before submitting.
>
> ⚠ GitHub issues are public. What you saw on the previous screen is all that is
> included, with email addresses, account IDs and your user name already removed.

The warning names the real risk. "Uploaded to the author" would be false — the
user is the one publishing, and publicly.

## What the report contains

| Section | Fields | Source |
| --- | --- | --- |
| MCS | version, OS, arch | `core.Version`, `runtime.GOOS/GOARCH`, OS version via `sw_vers`/`cmd /c ver` |
| Claude Desktop | version, install type | `updaterLastSeenVersion` in the profile's `config.json`; type from `platform` |
| Claude Code CLI | version | directory name under `<profile>/claude-code/` |
| Settings | auto sync on switch, login item | `core.AutoSyncOnSwitch()`, `core.LoginItemEnabled()` |
| Profiles | folder, account, signed in, running, org, conversation count | `panelui.BuildProfiles` + `platform.ProfileInfo.UUIDBuckets` + `platform.GetProfileActiveOrgUUID` |
| Active record | recorded profile identity | `core.LoadActiveProfile()` |
| Log | last 200 lines of each existing log file | `core.LogDir()` |

Install type is `standalone` / `store` / `macos`, from `platform.MSIXAvailable()`
and the build target — not guessed from paths at report time.

## Masking

Stable pseudonyms, not asterisks. `vin***@fontrip.com` still leaks a name and a
domain, and two occurrences cannot be told apart. A pseudonym leaks neither and
keeps the relationships, which is what a bug report is actually made of.

| Real | In the report |
| --- | --- |
| `vincent@fontrip.com` | `account-1` |
| `035899b2-b130-40b6-aa9e-93cf208df7b7` (account) | `account-1` |
| `d129c8c1-7834-4e6c-84a4-dc19dfeedc8f` (org) | `org-A` |
| `/Users/vincentkao/Library/Application Support/Claude` | `~/…/Claude` |
| `C:\Users\Adam Smith\AppData\Roaming\Claude` | `%USERPROFILE%\…\Claude` |

Rules:

- Accounts are numbered in the order they are first seen; organizations are
  lettered. The masker is told which values belong together — each profile
  contributes its account UUID and its email as one registration — so an email
  and the UUID of the same account collapse to one pseudonym, and `account-1`
  means the same account wherever it appears, including in log lines that only
  ever mention the UUID.
- The home directory prefix is replaced; the tail of the path is kept, because
  which folder inside the profile a file landed in is usually the bug.
- The same substitutions are applied to every log line, so a log cannot
  reintroduce what the summary removed.
- **Profile folder names are kept.** They are MCS's own construct and the axis
  every report is read along; masking them would leave the report unreadable. A
  user who names a profile after their employer is publishing that name, which is
  what the notice and the public-issue warning are for.

Masking is not optional. A bug that is *about* a path or an account name will not
be diagnosable from the report alone and will need a follow-up question. That is
the accepted cost.

## Submitting

`Copy and open` does two things:

1. Writes the report to the clipboard.
2. Opens `https://github.com/miou1107/multi-claude-switcher/issues/new?title=…&body=…`

The body carried in the URL is **not** the report — a prefilled issue URL is
limited to roughly 8 KB and 200 log lines will not fit. The URL carries a short
template ("Paste the report here with ⌘V / Ctrl+V") and the title carries the
first line of the user's comment, or `Problem report` when the comment is empty.
The report itself travels by clipboard. The user pastes once; that step cannot be
removed.

Clipboard: `pbcopy` on macOS, `Set-Clipboard` on Windows, reusing the existing
`psEnc`/`runPS` helpers. `navigator.clipboard` is not used — the panel is not
served over a secure origin on both hosts and a silent failure there would be
invisible.

Opening the URL: `exec.Command("open", url)` on macOS; the existing
`openURL` (`cmd/mcs-tray/openurl_windows.go`) on Windows.

If the clipboard write fails the browser is not opened, and the screen says so —
opening a blank issue form with nothing to paste is worse than doing nothing.

## Structure

The report builder is a new package, `core/diagnostics`, and is pure: it takes
already-gathered state and returns a string. It does no I/O beyond reading the
log files, and takes the log directory as a parameter so tests can point it
somewhere else.

```go
package diagnostics

type Input struct {
    Version      string
    OS, Arch     string
    OSVersion    string
    Install      string          // "standalone" | "store" | "macos"
    ClaudeVer    string
    ClaudeCodeVer string
    AutoSync     bool
    LoginItem    bool
    Profiles     []Profile
    ActiveRecord string
    LogDir       string
}

func Build(in Input) string
func NewMasker() *Masker            // stable pseudonyms, first-seen order
func (m *Masker) Apply(s string) string
```

Everything platform- or host-specific stays outside: each host gathers `Input`
the way it already gathers `SettingsVM`, and hands it over. That keeps the part
worth testing — the masking and the formatting — free of both hosts.

Renderer: `panelui.RenderDebug(vm DebugVM)` with `Report string` and
`Comment string`, following `RenderNewProfile`'s shape. Two new actions,
`showDebug` and `reportProblem`, added to `goPanelAction` and `dispatchAction`,
and a `debug` case in both `reloadPanel`s. The comment is JSON-encoded into the
single `arg` string the bridge allows, the way `createProfileSave` already does.

## Fixing macOS logging first

`cmd/mcs-menubar` never calls `core.SetupLogging`, so there is no log file on
macOS at all — `log.Printf` goes to stderr and is lost. With the log now a
mandatory part of every report, a macOS report would carry an empty log section.

`SetupLogging("mcs-menubar")` is added at menu-bar startup, matching
`mcs-tray`/`mcs-panel`. This is a prerequisite, not a side quest: without it the
feature is half-dead on the platform most users are on.

While there, `openLog` re-derives the log path inline in both hosts instead of
calling `core.LogDir()`, which means the fallback path in `LogDir` is unreachable
from the UI. Both are switched to `core.LogDir()`.

## Errors

Nothing in report-building is allowed to fail the screen. A field that cannot be
read is rendered as `unknown` with the reason, e.g.
`Claude Desktop: unknown (config.json unreadable: permission denied)`. A missing
log file renders as `mcs-menubar.log: not present`. A report that admits a gap is
useful; a screen that refuses to open is not.

## Testing

- `Masker`: same account via email and via UUID collapses to one pseudonym;
  numbering follows first-seen order; home prefix replaced on both platforms'
  spellings; a log line containing an email is masked identically to the summary.
- `Build`: a full input produces a stable string (golden); missing fields render
  as `unknown` rather than empty; the log section is truncated to 200 lines per
  file and says so.
- `RenderDebug`: the notice, both buttons and the comment box are present; the
  comment is HTML-escaped.
- Issue URL: title falls back to `Problem report` on an empty comment; the URL
  stays under 8 KB regardless of comment length (the comment is truncated for the
  title only).
- macOS logging: `SetupLogging` is called at menu-bar startup — asserted on the
  log file existing after start.

## Out of scope

- Any upload endpoint or server. If one-click reporting is ever wanted, only the
  submit step changes; the screen, the report and the masking do not.
- Reading the Claude Desktop version from the installed app rather than from
  `config.json`. One source is enough, and it is the one already being read.
- Masking profile folder names.
