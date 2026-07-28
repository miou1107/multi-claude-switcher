# How it works

Background for the curious. None of this is needed to use the app.

## Profiles are ordinary data directories

Claude Desktop keeps everything for one signed-in account in a single data
directory, and accepts a `--user-data-dir` flag choosing which one to use:

| | Path |
|---|---|
| macOS | `~/Library/Application Support/Claude*` |
| Windows | `%APPDATA%\Claude*` |

A "profile" is one of those directories. Switching accounts means closing Claude
Desktop and reopening it against a different one — which is why each account
stays signed in independently and why switching never asks you to log in again.

This is also why the **Microsoft Store (MSIX)** build is unsupported: it
virtualizes its storage and cannot be relaunched against a custom data
directory.

## Why a switch closes Claude Desktop

Claude Desktop reads its data directory at startup. There is no way to point a
running instance at a different one, so a switch has to close the app and
reopen it. The switcher does exactly that and nothing more — a plain switch
never reads or writes session data.

## How sync keeps sessions visible

The Code tab only lists conversations from the bucket named after the profile's
own logged-in account. A naive file copy would drop the source account's
sessions into a bucket the target app never reads, and they would silently
never appear.

So sync reads the source profile's account bucket and **re-homes** those
sessions under the *target* profile's account bucket. That is what makes a
cross-account sync actually surface in the target's sidebar (verified
on-device).

## Sign-in callbacks on Windows

Signing in opens a browser, and the browser hands the result back through a
`claude://` URL. Windows resolves that through a registry command that Claude
Desktop points at whichever profile it last launched — so a sign-in started
from a second profile could land back in the first one.

While a sign-in is pending, the switcher temporarily repoints that registry
command at the profile you are signing into, re-asserting it because Claude
Desktop re-registers the handler about 825 ms after it launches. The original
value is restored when the sign-in completes or the app exits.

Design notes: `superpowers/specs/2026-07-28-windows-signin-callback-design.md`.

## Where the app keeps its own data

Everything the switcher itself stores lives under `~/.multi-claude-switcher/`:

| Path | Contents |
|---|---|
| `backups/` | Timestamped session snapshots |
| `logs/` | Application logs |
| `settings.json` | Auto Sync and warning-dismissal state |
| `managed.json` | Which discovered accounts you chose to manage |
| `names.json` | Your custom display names for profiles |

It never writes anywhere else, apart from the login item when you enable
**Start at login**.
