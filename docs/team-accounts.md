# Team accounts are export-only

A Claude **Team** account can be a sync **source** but not a sync **target**.
This page is the evidence behind that claim; the summary lives in the
[README](../README.md#team-accounts-are-export-only).

## What was tested

Directly tested on-device, 2026-07-23, with a real Team account and a real
personal account:

- ✅ **Team → personal (export) works.** Copying a Team account's Code sessions
  into a personal account's folder makes them show up in the personal account.
- ❌ **Anything → Team (import) does NOT work.** Copying another account's
  session files into a Team account's folder does **nothing**. The sessions
  never appear in the Team account's sidebar — not after a relaunch, and not
  after a full cache wipe.

## Why

Claude Desktop builds a Team account's Code sidebar by **fetching the session
list from Anthropic's servers**, scoped to your account *and* organization: the
app calls `sessions_api_list_sessions` with an `orgUuid`, and Anthropic's own
documentation confirms session transcripts are stored server-side.

For a Team account the server is the source of truth, so local files copied
into its folder are simply not consulted. There is no user setting that makes a
Team account read local files instead.

**Net effect: you cannot bring a personal account's Code conversations into a
company Team account by copying files.** File-copy import only works where the
app treats local `claude-code-sessions/` files as authoritative, which is the
personal-account case.

Full probe transcript: `superpowers/specs/2026-07-22-probe-results.md`.

## How the app handles it

The switcher detects a Team account and warns you before an action that would
try to import into one — enabling Auto Sync, or picking a sync direction that
targets it. It does not block the action; the export half of a sync still works.

Detection is best-effort. An account the switcher cannot classify is left
untagged rather than mislabeled, so a missed warning is possible but a false one
is not. See `superpowers/specs/2026-07-23-team-account-detection-design.md`.
