package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

func printUsage() {
	fmt.Printf("multi-claude-switcher (mcs) CLI v%s\n", core.Version)
	fmt.Println("\nUsage:")
	fmt.Println("  mcs status                     Show detected profiles and running status")
	fmt.Println("  mcs backup [ProfileName]       Backup sessions for a profile")
	fmt.Println("  mcs sync <Src> <Dst>           Sync sessions from Src profile to Dst profile")
	fmt.Println("  mcs switch <Src> <Dst> [-y]    Perform safe switch from Src profile to Dst profile")
	fmt.Println("                                 Closes Claude Desktop, so it asks first; -y skips the prompt")
	fmt.Println("  mcs restore <BackupPath> <Dst> Restore sessions from backup")
	fmt.Println("  mcs help                       Show this help message")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	command := os.Args[1]

	// Persist a log trail for the mutating commands (read-only status/help stay
	// quiet). SafeSwitch and friends log their steps via the standard logger.
	switch command {
	case "switch", "sync", "restore", "backup":
		if closer, _, err := core.SetupLogging("mcs-cli"); err == nil {
			defer closer.Close()
		}
	}

	plat := platform.New()
	bm := core.NewBackupManager("")
	switcher := core.NewSwitcher(plat, bm)

	switch command {
	case "status":
		profiles, err := plat.FindProfiles()
		if err != nil {
			fmt.Printf("Error finding profiles: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Found %d Claude Desktop profile(s):\n", len(profiles))
		for _, p := range profiles {
			fmt.Printf("\n📁 Profile: %s\n", p.Name)
			fmt.Printf("   Path: %s\n", p.Path)
			if p.HasSessionsDir {
				fmt.Printf("   UUID Buckets (%d):\n", len(p.UUIDBuckets))
				for uuid, count := range p.UUIDBuckets {
					fmt.Printf("     - %s (%d sessions)\n", uuid, count)
				}
			} else {
				fmt.Println("   (No claude-code-sessions directory)")
			}
		}

		running, procs, _ := plat.IsAppRunning()
		fmt.Println("\n--------------------------------------------------")
		if running {
			fmt.Printf("🔴 Active Claude Desktop Processes (%d running):\n", len(procs))
			for _, proc := range procs {
				if len(proc) > 100 {
					proc = proc[:100] + "..."
				}
				fmt.Printf("   - %s\n", proc)
			}
		} else {
			fmt.Println("🟢 No Claude Desktop process currently running.")
		}

	case "backup":
		profiles, _ := plat.FindProfiles()
		if len(os.Args) >= 3 {
			profileName := os.Args[2]
			targetPath := resolveProfilePath(plat, profileName)
			backupPath, err := bm.CreateBackup(targetPath)
			if err != nil {
				fmt.Printf("Backup failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Backup created at: %s\n", backupPath)
		} else {
			for _, p := range profiles {
				if p.HasSessionsDir {
					backupPath, err := bm.CreateBackup(p.Path)
					if err == nil {
						fmt.Printf("Backed up %s -> %s\n", p.Name, backupPath)
					}
				}
			}
		}

	case "sync":
		if len(os.Args) < 4 {
			fmt.Println("Usage: mcs sync <SrcProfile> <DstProfile>")
			os.Exit(1)
		}
		src := resolveProfilePath(plat, os.Args[2])
		dst := resolveProfilePath(plat, os.Args[3])

		// Refuse to write while Claude is running: syncing into (or reading
		// from) a live-writing profile can corrupt the shared index. Use
		// `mcs switch` for the close-then-sync flow instead.
		if running, _, _ := plat.IsAppRunning(); running {
			fmt.Println("Claude Desktop is running. Quit it first, or use `mcs switch` which closes it safely.")
			os.Exit(1)
		}

		// Back up the destination first. Sync is additive and never replaces a
		// file the target already has, so this is not guarding against an
		// overwrite; it is so a run that adds hundreds of conversations can be
		// undone. Abort on a genuine backup failure rather than writing
		// unprotected.
		backupPath, berr := bm.BackupIfHasData(dst)
		if berr != nil {
			fmt.Printf("Aborting sync: failed to back up target (refusing to overwrite without a backup): %v\n", berr)
			os.Exit(1)
		}
		if backupPath != "" {
			fmt.Printf("Backed up target before sync: %s\n", backupPath)
		}

		report, err := core.SyncSessions(src, dst)
		if err != nil {
			fmt.Printf("Sync failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Sync complete! Re-bucketed %s -> %s. Copied: %d, Skipped: %d, Conflicts: %d\n", report.SourceAccount, report.TargetAccount, report.CopiedCount, report.SkippedCount, report.ConflictCount)
		if report.ConflictCount > 0 {
			fmt.Printf("⚠️  %d file(s) left untouched: the target has a different version and sync never replaces one:\n", report.ConflictCount)
			for _, c := range report.Conflicts {
				fmt.Printf("   - %s\n", c)
			}
		}

	case "switch":
		if len(os.Args) < 4 {
			fmt.Println("Usage: mcs switch <SrcProfile> <DstProfile> [-y]")
			os.Exit(1)
		}
		src := resolveProfilePath(plat, os.Args[2])
		dst := resolveProfilePath(plat, os.Args[3])

		// Switching closes the running Claude Desktop, so ask first. Same rule as
		// the panel: an app somebody is working in is not closed on one command
		// without their word.
		if !confirmClosingClaude(
			fmt.Sprintf("Switch to %s? Claude closes and reopens signed in as that account.",
				filepath.Base(dst))) {
			fmt.Println("Cancelled.")
			return
		}

		if err := switcher.SafeSwitch(src, dst, profileIdentityFor(plat, dst)); err != nil {
			fmt.Printf("Switch failed: %v\n", err)
			os.Exit(1)
		}

	case "restore":
		if len(os.Args) < 4 {
			fmt.Println("Usage: mcs restore <BackupPath> <DstProfile>")
			os.Exit(1)
		}
		backupPath := os.Args[2]
		dst := resolveProfilePath(plat, os.Args[3])

		// Restore overwrites the destination's live session index. Refuse while
		// Claude Desktop is open: renaming the sessions dir out from under a
		// live-writing app can corrupt it (same guard as `mcs sync`).
		if running, _, _ := plat.IsAppRunning(); running {
			fmt.Println("Claude Desktop is running. Quit it first before restoring (restore overwrites the live session index).")
			os.Exit(1)
		}

		if err := bm.RestoreBackup(backupPath, dst); err != nil {
			fmt.Printf("Restore failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Restore complete!")

	default:
		printUsage()
	}
}

// profileIdentityFor names the profile living at path, or "" when no known
// profile does. The identity is what MCS records as the active account, and it is
// not filepath.Base(path): on the Store build every profile shares one directory.
func profileIdentityFor(plat platform.Platform, path string) string {
	profiles, err := plat.FindProfiles()
	if err != nil {
		return ""
	}
	for _, p := range profiles {
		if platform.SamePath(p.Path, path) {
			return p.Name
		}
	}
	return ""
}

func resolveProfilePath(plat platform.Platform, nameOrPath string) string {
	if filepath.IsAbs(nameOrPath) {
		return nameOrPath
	}
	appSup := plat.AppSupportDir()
	return filepath.Join(appSup, nameOrPath)
}

// hasYesFlag reports whether -y / --yes was passed anywhere in the arguments.
func hasYesFlag() bool {
	for _, a := range os.Args[1:] {
		if a == "-y" || a == "--yes" {
			return true
		}
	}
	return false
}

// confirmClosingClaude gets the user's agreement before an operation closes their
// running Claude Desktop, and reports whether to go ahead.
//
// -y skips it, for scripts. Without -y and without a terminal to ask on, it
// refuses rather than assuming consent: a cron job that closes the app somebody is
// typing in should have said so explicitly.
func confirmClosingClaude(what string) bool {
	if hasYesFlag() {
		return true
	}
	fi, err := os.Stdin.Stat()
	interactive := err == nil && fi.Mode()&os.ModeCharDevice != 0
	if !interactive {
		fmt.Println(what)
		fmt.Println("This closes Claude Desktop. Re-run with -y to confirm (no terminal available to ask on).")
		return false
	}
	fmt.Println(what)
	fmt.Println("Anything unsaved in Claude is interrupted.")
	fmt.Print("Continue? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
