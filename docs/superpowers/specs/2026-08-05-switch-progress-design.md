# Progress: telling the user a long operation is running

Date: 2026-08-05

## The problem

Pressing Switch closes Claude Desktop, moves the user to another account, and
reopens it. That takes roughly a second and a half, during which the panel shows
one static line of text in the same green banner used for "done" messages, and
the account cards still look clickable. Nothing on screen says work is in
progress, so the panel reads as having ignored the click.

Two separate defects sit behind that:

1. The macOS host throws away `SafeSwitch`'s error (`doSwitch` in
   `cmd/mcs-menubar/main.go` calls it with `_ =`). A switch that fails looks
   exactly like a switch that worked. The Windows host already returns the
   error; macOS is the odd one out. This is the first finding recorded in
   [#16](https://github.com/miou1107/multi-claude-switcher/issues/16).
2. There is no way to render "an action is running" over the list. The panel has
   a modal for questions and a banner for results, and nothing for the interval
   between them.

## What is being built

A centred card, on the same scrim the confirmation dialog uses, in the same
position that dialog occupies. The user presses Switch inside the dialog and the
dialog is replaced in place by a progress card, so their eyes never have to move.

Three states, all of them the same card:

| State | Card |
|---|---|
| Working | Spinner, **Switching profile**, "Claude will restart in a moment." |
| Done | Green tick, **Switched successfully**, "You are now on `<name>`." |
| Done, with a warning | The same, plus what did not work, and a Close button |
| Failed | Red mark, **Switch failed**, the error text, and a Close button |

The fourth row is not decoration. `SafeSwitch` returns a non-nil error in cases
where the switch itself worked and only the optional session sync failed: the
target is open and the account has been recorded. Treating that as a failure
would put "Switch failed" over an account list already showing the target as
current, which is the panel contradicting itself with the wrong half winning.
`core.SwitchedWithWarning` marks those, and `panelui.SwitchOutcome` is the one
place that reads it, shared so the two hosts cannot disagree about whether a
switch happened.

The done card dismisses itself after a short delay and returns to the list, where
the target row is now marked as the current account. The failed card waits for
the user, because an error the user never read is an error that did not get
reported.

The list stays rendered behind the scrim. It is visible but unreachable: the
scrim is what stops a second click, rather than the silent `getBusy()` guard
doing it invisibly.

### Wording

The message deliberately does not enumerate the internal steps. The real
timings are lopsided: closing Claude takes about a second, aligning sessions is
skipped entirely unless auto sync is on, and launching returns as soon as the
process is spawned rather than when Claude appears. A four step checklist would
flash through three of its steps instantly, which reads as a fake progress bar.
One honest sentence about what is happening is better than four that are timed
wrong.

What the card must not do is claim success when the switch failed. That is the
reason defect 1 above is in scope: the success wording is only truthful once the
host can tell the two apart.

## Design

### `internal/panelui`

`ProgressVM` carries the state, and is deliberately not switch-specific:

```go
type ProgressVM struct {
    Phase   ProgressPhase // ProgressWorking | ProgressDone | ProgressFailed
    Title   string        // "Switching profile", "Sync finished"
    Detail  string        // the sentence under it
    Warn    string        // it worked, but; read only when Phase is ProgressDone
    Err     string        // read only when Phase is ProgressFailed
    Dismiss string        // where Close lands; allowlisted, "" means the list
}
```

`WithProgress(page, vm)` lays the card over an already-rendered page rather than
each renderer taking a view model. Four renderers each taking a parameter is
four places for one host to pass it and the other to forget; this way every
screen gets it from one call in each host's reload, and a fifth screen needs no
change at all. A nil VM returns the page untouched.

Each operation supplies its own copy through a `*Starting` / `*Outcome` pair
(`SwitchStarting`, `SwitchOutcome`, `SyncStarting`, `SyncOutcome`,
`MergeStarting`, `MergeOutcome`, `BackupStarting`, `BackupOutcome`). Those are
pure functions over what the operation returned, so every wording and every
phase decision is testable without a host, and the two hosts cannot word the
same outcome differently.

`Dismiss` is written straight into an `onclick`, so it is checked against a
short allowlist rather than escaped: escaping is exactly what failed in v0.9.1.

### Hosts

Both hosts hold one progress state next to the panel state they already keep,
and apply it in one place: the last line of the reload, for every view.

Navigating clears it, the way the rename editor state is cleared, and that
covers the card's own exits, since Close and the auto dismiss both send an
ordinary navigation action. What must NOT clear it is the panel opening or
being dismissed, which also set the view: doing so made an operation in flight
vanish the moment the user pressed Escape, and reported a failure that landed
while the panel was closed precisely nowhere. Nor may the merge goroutine, which
moves to the list on its way to putting its own outcome card up, nor the render
path's merge fallback, which fires when the plan cannot be computed against
accounts a merge is halfway through archiving. Those three go through
`setViewKeepingProgress` instead.

On macOS the stickiness flag is written under the same lock as the state it
describes, and read synchronously by the popover as it opens rather than
dispatched to. Released early, two goroutines' updates could land in the wrong
order and leave the popover stuck open with no card on screen; dispatched, the
flag would arrive after the popover was already shown, which is too late,
because AppKit installs the transient popover's event monitor at show time.

The panel is also stopped from closing itself while the card is up. Both hosts
dismiss the panel when something else takes focus, and a switch ENDS by
launching Claude Desktop, so Claude coming to the front was dismissing the card
that was reporting on it. macOS switches the popover from Transient to
ApplicationDefined; Windows skips its park on WA_INACTIVE. Escape and the menu
bar icon still close the panel on both, so nothing here can trap the user.

The rename editor's reload hold is overridden while a card is on screen.
Renaming one row does not stop the user clicking another and switching to it,
and holding the reload swallowed the card entirely: no sign of the switch while
it ran, then a stale success card appearing whenever the edit happened to end.

The sequence for a switch becomes:

1. Panel sends `switch`. The busy guard rejects a second one, unchanged.
2. Host sets phase working and reloads. The card appears over the list.
3. `SafeSwitch` runs. On macOS its error is now returned rather than discarded.
4. Host sets phase done or failed and reloads.
5. Done auto dismisses from the page via `showList`. Failed waits for Close,
   which sends the same `showList`.

If the panel is closed and reopened mid switch, the host still holds the working
phase, so the card is rendered again rather than the panel looking idle while
Claude is shut.

### The other three operations

Sync, Merge and Backup follow the same five steps, differing only in their copy
and in where their card dismisses to (Sync back to Sync, Backup back to
Settings, Merge to the account list, since after a merge the merge screen is
about two accounts that are now one).

Sync is the one that needed this most. It takes longer than a switch, it has a
real result to report (how many conversations moved, how many were already
newer, how many files could not be read), and its outcome was the most reliably
lost: there is a comment in the sync code saying exactly that, with a system
notification bolted on to rescue the one case nobody could afford to miss. The
notification stays, because it also reaches a user who has walked away from the
panel, but it is no longer the only way that message survives.

Files that could not be read are a warning, not a failure. The run continued
past them deliberately and the conversations that did copy really did copy.
`core.SyncResultParts` splits the summary from the skipped-file sentence so the
card can put them in different places while both still come from one source.

## Out of scope

- Waiting for Claude Desktop to actually appear before saying the switch is
  done. The user chose not to: it would add three to eight seconds to every
  switch to buy a more literal message.
- Drawing the outcome when the user asked to quit mid switch. Quit waits for the
  operation to finish and then exits, so the card that would report the result
  is never drawn. The user asked to leave; the log still has it.
- Removal. It already has a result screen of its own, added in 0.13.0, and
  folding that into the card is a separate change with its own wording to get
  right.

## Verification

- Unit tests over the three card states, including that the failed card shows
  the error text and a Close button and the done card does not.
- A test that a nil VM renders no scrim, so the ordinary list is unchanged.
- The em dash guard gains fixtures for all three states.
- On macOS, a real switch driven from the running app: the card appears, the
  account changes, and a switch to a folder that has been renamed away shows the
  failure card rather than a tick.
