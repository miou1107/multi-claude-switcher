# Windows: making the sign-in callback land in the right profile

Status: accepted 2026-07-28.

## Problem

Switching to a profile that has never been signed in to opens Claude Desktop on
it correctly, but completing the sign-in drops the new account into the
**default** profile instead. The user appears to be thrown back to the account
they switched away from.

Observed on a real machine, with timestamps that make the mechanism plain:

| Fact | Evidence |
| --- | --- |
| Claude was running on the target profile | `claude.exe --user-data-dir=…\ClaudeWork` |
| The target profile never received credentials | `ClaudeWork\config.json` stayed 158 bytes, no `oauth:tokenCache` |
| The default profile was written 24 s later | `Claude\config.json` modified at the moment sign-in completed |
| A second Claude was launched by the shell | `claude.exe "claude://login/google-auth?code=…"`, no `--user-data-dir` |

Cause: sign-in finishes in the browser, which hands the result back through the
`claude://` URL scheme. Windows resolves that with
`HKCU\Software\Classes\claude\shell\open\command`, which Claude Desktop
registers as:

```
"…\claude.exe" "%1"
```

`--user-data-dir` binds only the process the switcher starts. It cannot bind a
process the shell starts, and the registered command carries no profile, so the
callback always opens the default one.

The Store (MSIX) build is unaffected: it swaps profile folders so the active
profile always *is* the default one, which is why its callbacks land correctly.

## The part that makes this non-obvious

The first implementation pointed the handler at the profile just before
launching Claude. It did not work, and the logs showed why:

```
22:07:09  claude:// handler now opens …\ClaudeWork     (we wrote it)
22:07:29  Claude\config.json written                   (sign-in landed in the default profile anyway)
later     handler back to "…claude.exe" "%1"           (nobody in the switcher restored it)
```

Measured directly: **Claude Desktop re-registers its own protocol handler about
825 ms after it starts**, overwriting whatever is there. Anything written before
launch is gone before the user has even seen the sign-in screen.

So the write has to land *after* Claude's registration, and be re-asserted if
Claude does it again.

## Approach

After switching to a profile that has **no account yet**, hold the handler on
that profile:

```
"…\claude.exe" --user-data-dir="…\ClaudeWork" "%1"
```

A poll every second re-asserts it (a no-op unless Claude has clobbered it) and
watches the profile for its new account. The hold ends the moment the account
appears, or after 10 minutes, and restores the handler either way.

A profile that already has an account is never touched: no callback is needed,
so there is nothing to steer. That confines the registry write to the seconds
around a sign-in rather than the whole session.

The alternative considered was giving the standalone build the same
folder-swapping mechanism the Store build uses, which removes the problem
rather than working around it. That is the more thorough fix, and it is the one
to reach for if this proves fragile; it was not taken now because it turns every
switch into a rename of the user's real profile directories.

## Keeping it contained

This writes to the registry, so the failure modes were designed out rather than
documented away:

- **Per-user only.** `HKCU`, no elevation, no machine-wide effect.
- **Nothing is remembered.** The executable path is re-read from the current
  value on every write, and Restore rewrites the pristine form rather than
  replaying a stored backup. A Claude Desktop update that moves the exe
  therefore cannot leave a stale path behind: the next write picks up the new
  one, and Claude's own installer rewriting the key simply restores it.
- **Idempotent.** Pointing at the profile already named is a no-op, so repeated
  switches cannot accumulate arguments.
- **Non-fatal.** A failure to rewrite is logged and the switch proceeds. The
  cost is a misdirected sign-in, not a failed switch.
- **Self-limiting.** The hold ends on success or after 10 minutes, and a second
  switch supersedes the first rather than the two fighting.
- **Restored on exit**, so the switcher leaves nothing behind while it is not
  running to maintain it.
- **Releasing cancels the hold first.** Restoring while a hold is still polling
  is undone a second later by that hold, which silently leaves the handler
  pointed after the switcher believes it has cleaned up. This was observed in
  testing, which is why callers outside the file use
  `ReleaseProtocolHandlerHold` rather than `RestoreProtocolHandler`.

Residual risks, accepted:

- If the panel process is killed mid-hold, the handler stays pointed until the
  next switch or a clean tray exit restores it. It points at the profile the
  user was last on, so `claude://` links still open something sensible.
- A profile whose token expires and needs a fresh sign-in is not covered: it
  already has an account, so no hold is taken, and its callback would land in
  the default profile. Re-running the switch onto it is the workaround.

## Testing

The registry write cannot be unit-tested without touching the machine, so the
parsing and rendering that decide *what* gets written are pure and covered:
extracting the exe from quoted and unquoted forms, rendering both the pointed
and pristine commands, reading a profile back out, and a round trip proving two
rewrites do not accumulate arguments or lose the exe.

The write path was verified on a real machine in two passes:

1. Record the value, point it at a profile, re-point it at the same profile
   (must not change), restore, confirm it matches the original byte for byte.
2. Take a hold, launch Claude on the profile, and watch: the handler must be
   clobbered by Claude at ~800 ms and reclaimed by the hold at ~1100 ms. Then
   release it and keep watching, to confirm the hold does not re-apply it.

The second pass is the one that matters. The first version of this change
passed pass 1 and still did not work, because pass 1 never involves Claude
starting.
