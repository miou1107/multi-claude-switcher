# Team accounts: what was wrong, and what was actually happening

This page used to say a Claude **Team** account could be a sync source but never a
sync target. That was wrong. It was corrected on 2026-08-04, and this page is kept
rather than deleted because the way the mistake was made is worth having on record.

The summary lives in the [README](../README.md#team-accounts-sync-like-any-other).

## What is true

Conversations sync into a Team account. Nothing about a Team account behaves
differently from a personal one.

## What the bug was

Code sessions are filed on disk under two identifiers, not one:

```
claude-code-sessions/<accountUuid>/<orgUuid>/local_<sessionId>.json
```

Claude Desktop reads exactly one of those organization folders: the one the signed-in
account is currently working in.

Sync rewrote the **account** segment to the target's account, which is what makes
history follow you across accounts, and left the **organization** segment naming
the source's organization. So a personal account's conversations copied into a
company Team account landed at:

```
claude-code-sessions/<teamAccount>/<personalOrg>/…     ← written here
claude-code-sessions/<teamAccount>/<teamOrg>/…         ← read from here
```

Every file arrived, complete and uncorrupted, in a folder that account never opens.
From the outside this is indistinguishable from an import being refused.

## How it was diagnosed wrongly on 2026-07-23

Two probe conversations were created, synced into a Team account, and did not appear
in its sidebar. That much was correctly observed. The conclusion drawn from it was
not:

> Claude Desktop builds a Team account's Code sidebar by fetching the session list
> from Anthropic's servers, so local files are never consulted.

The evidence for that was 98 log lines containing a `sessions_api_list_sessions`
query key with an `orgUuid`. Re-read a fortnight later, that evidence does not
support the claim:

- All 98 lines are from a **single day** (2026-06-26), not from the probe.
- All 98 are `Missing queryFn` **errors** — React Query reporting it had no fetch
  function for that key, which indicates the data was populated from somewhere else,
  not that a request was made.
- The `orgUuid` in them is the **personal** organization, not the Team one the claim
  was about.
- The only genuine server calls in the whole log, three `GET /v1/code/sessions`, are
  also from June and all returned 503.

A conclusion about Team accounts was built from errors, logged on one unrelated day,
about a personal account.

## What was measured on 2026-08-04

The two probe files from July were still on disk, in
`<teamAccount>/<personalOrg>/`, which the app had never read. One of them,
titled `SYNC-REAL-9`, was copied unchanged into `<teamAccount>/<teamOrg>/` and the
Team profile was relaunched:

- The app's own log went from `Loaded 248 persisted sessions` to `Loaded 249`, the
  difference being exactly the copied file.
- `SYNC-REAL-9` appeared in the Team account's Code sidebar, opened, and showed its
  full transcript.

So the sidebar is built from local files, for a Team account, and an imported
conversation is picked up like any other.

The fix was then checked against the real thing rather than only against test
fixtures: three real conversations plus both profiles' real `config.json` were
copied into a scratch directory and synced. They landed in
`035899b2…/d129c8c1…`, the exact folder the Team app's log shows it reading.

## How the active organization is determined

Anthropic does not publish this, so it is read from a side effect: `config.json`
carries a `dxt:allowlistLastUpdated:<orgUuid>` stamp per organization, and only the
signed-in one is refreshed at launch. Across two profiles and four organizations,
each launch updated exactly the stamp of the organization whose session folder that
profile then read.

It is a heuristic on a private format, so it fails loudly: when the organization
cannot be read from either profile, sync keeps the paths exactly as it did before
rather than filing conversations somewhere arbitrary. Being wrong there costs
visibility, never data.

## Confirmed against something outside the heuristic

The evidence above is one source read two ways: the stamps, checked against which
folder the app's own log said it loaded. That is weaker than it looks, because a
wrong reading of the stamps would agree with itself.

There is an independent source. The Claude Code process running inside a profile
carries its organization in its own command line:

```
--plugin-dir …\local-agent-mode-sessions\skills-plugin\<orgUuid>\<accountUuid>
```

Checked on a Windows machine whose profiles carried **three** organization stamps
each, not the two this was originally measured on, the organization named there is
the one the newest stamp names, on both profiles. The newest-wins rule had never
been exercised past two candidates before that.

On the same machine, the account receiving a sync went from 2 conversations to 101
in the folder it actually reads. The 99 that appeared were already on disk,
filed under the source's organization; nothing was downloaded or recreated.

See `platform/activeorg.go` and the organization re-bucketing in `core/sync.go`.

## The lesson worth keeping

The July probe was a real experiment with a real negative result. What went wrong
was the explanation attached to it: a log was searched for something that would
explain the failure, the first plausible match was accepted, and its date, its
severity and its subject were never checked. A second explanation — "the files are
in the wrong folder" — was never tested, even though the folder layout was already
known and written down in the same document.
