# Phase 0 Probe Findings Report

- Date: 2026-07-22 10:29:55
- Platform: macOS Darwin (Apple Silicon / Intel)
- Status: Phase 0 Validation Complete

## Executive Summary

The Phase 0 probe tests confirmed that **Claude Desktop on macOS fully respects the `--user-data-dir` argument**, enabling isolated multi-account profile execution. Furthermore, the conversation session files are stored under `claude-code-sessions/<AccountUUID>/<OrgUUID>/local_<SessionUUID>.json`, confirming that index sync and Safe Switch mode are technically viable.

---

## Probe Validation Checklist (Questions 1 - 10)

### Q1: Does Claude Desktop accept `--user-data-dir` at launch?
- **Result**: **PASS**
- **Findings**: `open -n -a "Claude" --args --user-data-dir=/tmp/claude_probe_test/profile_q1` launched successfully and created a complete profile tree (Cookies, Local Storage, Preferences, etc.).

### Q2: Is `claude-code-sessions/<UUID>` the primary source for conversation sessions?
- **Result**: **PASS**
- **Findings**: The directory structure is `<AccountUUID>/<OrgUUID>/local_<SessionUUID>.json`. Each `.json` file contains full session metadata and turn content (`top_keys`: ['sessionId', 'cliSessionId', 'cwd', 'originCwd', 'lastFocusedAt', 'createdAt', 'lastActivityAt', 'model', 'effort', 'isArchived']).

### Q3: Do synced/copied session files appear in the target profile's sidebar on app relaunch?
- **Result**: **PASS**
- **Findings**: Copying `local_*.json` into the target profile's matching `<AccountUUID>/<OrgUUID>/` directory causes Claude Desktop to display the shared conversation upon app restart.

### Q4: How does Claude Desktop handle UUID buckets that don't match the active account?
- **Result**: **ISOLATED**
- **Findings**: Each account only queries its corresponding `<AccountUUID>` bucket. Syncing between accounts requires copying/re-bucketizing sessions under the target account's `<AccountUUID>`.

### Q5: Does the app rebuild or overwrite `claude-code-sessions` on restart or crash?
- **Result**: **SAFE**
- **Findings**: Session files are written incrementally per conversation turn (`local_*.json`). Restarting or launching does not wipe existing session files.

### Q6: Do macOS Symlinks in `claude-code-sessions` survive app restarts?
- **Result**: **PARTIAL / USE WITH CAUTION**
- **Findings**: Symlinks function during reading, but atomic file writes by Electron can break directory symlinks into standalone folders during writes. Safe Switch (close-then-sync) remains the primary recommended mode.

### Q7: Can the sidebar render from local files without active network connection?
- **Result**: **PASS**
- **Findings**: Desktop sidebar renders past session list locally from disk.

### Q8: For the same conversation across two accounts, are UUID buckets identical or distinct?
- **Result**: **DISTINCT**
- **Findings**: Each account profile uses its own `<AccountUUID>`. Shared sessions need to be indexed under both `<AccountUUID>` directories.

### Q9: Does the server accept continuing session context across account switches?
- **Result**: **PASS (Local continuity)**
- **Findings**: Local UI displays full conversation history. New prompt turns generate new API requests under the current active account's token.

### Q10: Windows MSIX compatibility (Backlog)
- **Result**: **BACKLOG**
- **Findings**: Windows testing deferred to Phase 1/2. macOS Darwin is primary target.

---

## Discovered Profiles & UUID Structure

```json
[
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude",
    "exists": true,
    "has_sessions_dir": true,
    "uuids": {
      "ae543f88-0f24-4ae6-ae21-3033915bca76": {
        "count": 82,
        "files": [
          "local_ba78450e-9db2-4089-8ee6-39d098a772c5.json",
          "local_34bc6651-485d-4be8-a4cc-43fdbcf4dfac.json",
          "local_1ced648d-5048-4e49-8c54-a7b32195fa46.json",
          "local_e88d1ef5-55fe-4039-9235-d76277a44395.json",
          "local_74789b2b-6b63-4e30-8c98-ddeee553241f.json"
        ]
      },
      "f047dab6-372f-4505-94a1-92e1bc507657": {
        "count": 19,
        "files": [
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_72ce8561-5359-448b-abeb-8030289c1230.json",
          "local_a023d8e8-8d79-4d0c-9b1a-ba8769f2852c.json",
          "local_c253f7bd-6c51-47fc-a58e-533cc9e9e7ca.json",
          "local_85e8de3a-6513-4fae-a34a-082a5fdea86d.json"
        ]
      },
      "035899b2-b130-40b6-aa9e-93cf208df7b7": {
        "count": 236,
        "files": [
          "local_2485e49d-ca86-4d37-b663-5fa4195a4159.json",
          "local_c32beae8-6da4-4ac3-b8b0-e5ca120ef39f.json",
          "local_c88a6513-1c6b-4324-bf3b-e42ed7960854.json",
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_8c79f948-81ff-4faf-b0e4-6fc377989ea1.json"
        ]
      }
    }
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude-3p",
    "exists": true,
    "has_sessions_dir": false,
    "uuids": {}
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/ClaudeBar",
    "exists": true,
    "has_sessions_dir": false,
    "uuids": {}
  },
  {
    "path": "/Users/vincentkao/Library/Application Support/Claude_Profile2",
    "exists": true,
    "has_sessions_dir": true,
    "uuids": {
      "f047dab6-372f-4505-94a1-92e1bc507657": {
        "count": 2,
        "files": [
          "local_7cace653-e224-4996-acdb-891b31cb6726.json",
          "local_03465ecf-cd16-409c-9beb-85964dddf3d5.json"
        ]
      },
      "035899b2-b130-40b6-aa9e-93cf208df7b7": {
        "count": 233,
        "files": [
          "local_2485e49d-ca86-4d37-b663-5fa4195a4159.json",
          "local_c32beae8-6da4-4ac3-b8b0-e5ca120ef39f.json",
          "local_c88a6513-1c6b-4324-bf3b-e42ed7960854.json",
          "local_1a816eb3-ed89-41a2-94cb-5bf984176b74.json",
          "local_8c79f948-81ff-4faf-b0e4-6fc377989ea1.json"
        ]
      }
    }
  }
]
```

---

## Conclusion & Architecture Confirmation

1. **Safe Switch Mode (Close -> Backup -> Sync Index -> Launch Target Profile)** is validated as the optimal, zero-risk default.
2. **Phase 1 Go Core CLI** will focus on:
   - Process detection & clean shutdown (`pkill -f Claude`)
   - Automated profile backup (`/tmp/claude_probe_backups/`)
   - `claude-code-sessions` index copier/reconciler
   - `--user-data-dir` launcher for target profiles
