# Slim the Settings screen

## Why

Settings had grown to ten rows, eight of them buttons stacked one under another,
with no grouping and no visual difference between "change a preference", "do something
now" and "go to another screen". Three of the eight were folder shortcuts almost nobody
opens.

The result is a wall. Nothing on it is wrong; there is just too much of it for a panel
this size.

## What it becomes

Settings holds what a person actually adjusts:

- **Auto Sync on switch** (toggle)
- **Start at login** (toggle)
- **More ›**
- **Quit Multi-Claude Switcher** (red, on its own)

and a quiet line beneath: `v0.13.2 · Check for updates · Report a bug`.

**More** holds the four things that act on conversation data:

- **Sync between accounts ›**
- **Back up all accounts**
- **Open backup folder**
- **Open archive folder**

## The rules the symbols follow

They were inconsistent before, so they are stated here rather than left to taste.

- **`›`** means this opens another screen: More, and Sync between accounts.
- **`…`** means this does something or raises a dialog. Nothing on Settings needs one now.
- **No mark** means it happens on the spot: the backup, the two folder shortcuts.

`›` on the right also mirrors the `‹` back button on the left, so the two read as a pair.

Ellipses were removed from the two navigation rows. A `›` already says "goes somewhere";
a `…` beside it says the same thing twice.

## What moved out of sight, and why that is acceptable

**Check for updates** and **Report a bug** are now quiet text in the footer rather than
full-width buttons. Neither is a daily action. Putting the update check beside the version
number also puts it where somebody is already looking when the question occurs to them.

They are still `<button>` elements, drawn flat. Anchors were tried first and were wrong:
an `<a>` with no `href` is not a tab stop and does not answer Enter, and the bug report
screen is reachable ONLY from here now, so anchors would have put it beyond anyone not
using a mouse.

**Quit stays a button.** It was offered as footer text and as half a shared row, and both
were declined: it is a thing people deliberately go looking for, and this project's own
guidance keeps a destructive control red and away from its neighbours. Sharing a row with
More would have put the most-pressed control next to the one that must not be mis-pressed.

**Open log folder is removed outright.** The last lines of every log already travel inside
the bug report, which is the reason anyone would have opened the folder. Anybody who needs
the whole file can still find it; nobody needs a button for it.

## Naming

**Debug info** becomes **Report a bug**, on the screen's own heading as well as the link
that reaches it. The screen already ends in a button that files a GitHub issue, so its old
name described what it shows rather than what it is for.

**Sync sessions…** becomes **Sync between accounts ›**. Inside a submenu reached
deliberately, "manual" is not worth a word: the screen it opens is the manual one, and
"between accounts" says what it actually does.

**More** was chosen over **Conversations**, which was tried first. One of the four rows
opens the archive folder, which holds removed accounts rather than conversations, so
"Conversations" over-promised. "More" promises nothing and cannot be wrong.

## Implementation

`RenderMore` joins the other renderers in `internal/panelui`. Both hosts gain a `showMore`
action and a `more` view, and lose `openLog`.

`SettingsVM` keeps `Busy`, which now disables the footer's update check rather than a
full-width button. `MoreVM` carries only `Busy`: the backup that lives there reports
through the progress card rather than a banner, and `showMore` clears any leftover banner
on the way in.

The chevron's class is `.navchev`, not `.chev`. That name was already the account list's
switch chip and the sync screen's direction chip, and adding a second rule for it repainted
both from lilac to grey.

The Sync screen's back button now returns to More, which is where it is entered from.

`internal/hostparity` already fails when the two hosts stop offering the same set of panel
actions, which is what makes adding one action and removing another across two files safe
to do in one pass. This is the first change to rely on it.

## Tests

- every row leads somewhere: each action the rendered Settings and More screens send is
  handled by both hosts (already enforced by `internal/hostparity`)
- Settings no longer offers the actions that moved, and does offer `showMore`
- More offers exactly the four that moved
- `openLog` is gone from the page and from both hosts
- the busy state disables the update check and the backup, and nothing else
- the em dash guard covers both screens in both their idle and busy states, and
  fails when a renderer exists with no fixture at all. Review found the first
  version of this claim false twice over: the guard's own screen list omitted
  More, so an em dash there passed the whole suite, and the test named as
  providing the coverage checks progress-card placement rather than copy
- no class is styled from two places. Adding a second `.chev` rule for the
  navigation chevron repainted the account list's switch chip and the sync
  screen's direction chip from lilac to grey, which nothing would have caught

Each verified by breaking what it guards, with the broken version confirmed to compile.

## Version

A visible change to the panel with no change to behaviour, so a patch bump. It rides along
with 0.13.2, which is unreleased.
