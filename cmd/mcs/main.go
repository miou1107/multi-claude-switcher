package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/platform"
)

const Version = "0.1.0"

func printUsage() {
	fmt.Printf("multi-claude-switcher (mcs) CLI v%s\n", Version)
	fmt.Println("\nUsage:")
	fmt.Println("  mcs status                     Show detected profiles and running status")
	fmt.Println("  mcs backup [ProfileName]       Backup sessions for a profile")
	fmt.Println("  mcs sync <Src> <Dst>           Sync sessions from Src profile to Dst profile")
	fmt.Println("  mcs switch <Src> <Dst>         Perform safe switch from Src profile to Dst profile")
	fmt.Println("  mcs restore <BackupPath> <Dst> Restore sessions from backup")
	fmt.Println("  mcs help                       Show this help message")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	command := os.Args[1]
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

		report, err := core.SyncSessions(src, dst)
		if err != nil {
			fmt.Printf("Sync failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Sync complete! Copied: %d, Skipped: %d\n", report.CopiedCount, report.SkippedCount)

	case "switch":
		if len(os.Args) < 4 {
			fmt.Println("Usage: mcs switch <SrcProfile> <DstProfile>")
			os.Exit(1)
		}
		src := resolveProfilePath(plat, os.Args[2])
		dst := resolveProfilePath(plat, os.Args[3])

		if err := switcher.SafeSwitch(src, dst); err != nil {
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
		if err := bm.RestoreBackup(backupPath, dst); err != nil {
			fmt.Printf("Restore failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Restore complete!")

	default:
		printUsage()
	}
}

func resolveProfilePath(plat platform.Platform, nameOrPath string) string {
	if filepath.IsAbs(nameOrPath) {
		return nameOrPath
	}
	appSup := plat.AppSupportDir()
	return filepath.Join(appSup, nameOrPath)
}
