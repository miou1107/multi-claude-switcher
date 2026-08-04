# CLI reference

> **The CLI is not included in releases.** The macOS and Windows downloads
> contain the menu-bar / tray app only. To get `mcs` you have to
> [build it from source](building.md):
>
> ```bash
> go build -o bin/mcs ./cmd/mcs
> ```

Profile arguments are folder names (e.g. `Claude`, `Claude_Profile2`), not full
paths.

## `mcs status`

Show the detected profiles and any running Claude Desktop processes.

```bash
./bin/mcs status
```

## `mcs backup`

Take a timestamped snapshot of every profile's session index.

```bash
./bin/mcs backup
```

Snapshots land in `~/.multi-claude-switcher/backups/`.

## `mcs sync <source> <target>`

Copy the source profile's Code sessions into the target profile, without
changing which account you are signed in on. Backs the target up first.

```bash
./bin/mcs sync Claude Claude_Profile2
```

Team accounts sync in both directions like any other. They were once believed not
to; see [team-accounts.md](team-accounts.md) for what was actually going wrong.

## `mcs switch <source> <target>`

Safe switch: close the running app, back up, sync, then launch the target
profile.

```bash
./bin/mcs switch Claude Claude_Profile2
```

## `mcs restore <backup-path> <target>`

Restore a snapshot into a profile.

```bash
./bin/mcs restore ~/.multi-claude-switcher/backups/Claude_20260722_103206 Claude
```
