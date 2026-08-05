# Switch progress: telling the user the switch is running

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
| Failed | Red mark, **Switch failed**, the error text, and a Close button |

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

`SwitchProgressVM` carries the state:

```go
type SwitchProgressVM struct {
    Phase  SwitchPhase // SwitchWorking | SwitchDone | SwitchFailed
    Target string      // display name of the account switched to
    Err    string      // failure text; only read when Phase is SwitchFailed
}
```

`RenderList` gains a `switching *SwitchProgressVM` parameter. Nil renders the
list exactly as it renders today, so every existing caller and test keeps its
current meaning.

Rendering the card is a pure function over the VM, so the three states are
testable without a host.

### Hosts

Both hosts hold the progress state next to the busy flag they already keep, and
both clear it on `setView`, the way the rename editor state is cleared.

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

## Out of scope

- Waiting for Claude Desktop to actually appear before saying the switch is
  done. The user chose not to: it would add three to eight seconds to every
  switch to buy a more literal message.
- The same treatment for sync, merge, backup and removal. They have the same
  gap, but each has its own confirmation flow and its own failure wording, and
  doing them together would make one change impossible to review.

## Verification

- Unit tests over the three card states, including that the failed card shows
  the error text and a Close button and the done card does not.
- A test that a nil VM renders no scrim, so the ordinary list is unchanged.
- The em dash guard gains fixtures for all three states.
- On macOS, a real switch driven from the running app: the card appears, the
  account changes, and a switch to a folder that has been renamed away shows the
  failure card rather than a tick.
