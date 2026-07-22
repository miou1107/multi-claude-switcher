# Phase 0: Probe 探針實驗詳細執行計畫

> **For Antigravity:** REQUIRED SUB-SKILL: Load executing-plans to implement this plan task-by-task.

**Goal:** 透過自動化/半自動化驗證腳本與手動驗證步驟，回答 Spec 中 Phase 0 的 10 個核心未知問題，確認 Safe Switch 與 Symlink 模式在 macOS 下的技術可行性與邊界。

**Architecture:** 撰寫輕量 Python 探針工具（`scripts/probe/probe_runner.py`），在不破壞正式帳號 profile（`~/Library/Application Support/Claude*`）的前提下，建立隔離測試目錄進行對話索引結構檢視、進程檢測、與同步/Symlink 實驗。

**Tech Stack:** Python 3 (standard library), macOS `open` / `ps` / `kill` CLI commands.

---

### Task 1: 探針實驗環境與基礎腳本建立

**Files:**
- Create: `scripts/probe/probe_runner.py`

**Step 1: Write probe runner script**
撰寫 Python 腳本，具備以下模組：
- 檢查 `~/Library/Application Support/Claude` 與 `~/Library/Application Support/Claude_Profile2` 是否存在。
- 列出 `claude-code-sessions/` 裡面的 UUID 資料夾與檔名清單。
- 檢測 `Claude` Electron 進程 pid。
- 提供安全備份與測試 Profile 隔離建立功能。

**Step 2: Run script to verify environment and existing profiles**
Run: `python3 scripts/probe/probe_runner.py --status`
Expected output: 顯示兩個 Profile 的真實路徑、UUID 雜湊/檔名數量與目前的進程狀態。

---

### Task 2: 執行 Probe 驗證項目 (Q1 - Q5)

**Files:**
- Modify: `scripts/probe/probe_runner.py`
- Create: `docs/superpowers/specs/2026-07-22-probe-results.md`

**Step 1: Q1 Test --user-data-dir**
使用腳本在 `/tmp/claude_probe_temp` 建立臨時 profile，執行：
`open -n -a "Claude" --args --user-data-dir=/tmp/claude_probe_temp`
驗證 Claude Desktop 是否在 `/tmp/claude_probe_temp` 產生完整目錄結構與 Cookies。

**Step 2: Q2 & Q3 Sync & Sidebar Visibility Test**
在測試 profile 之間複製對話 index JSON（或設定 symlink），啟動測試 app 觀察側邊欄是否載入目標對話。

**Step 3: Document findings for Q1-Q5**
記錄測試結果至 `docs/superpowers/specs/2026-07-22-probe-results.md`。

---

### Task 3: 執行 Probe 驗證項目 (Q6 - Q10) 與文件歸檔

**Files:**
- Modify: `docs/superpowers/specs/2026-07-22-probe-results.md`
- Create: `CHANGELOG.md`
- Create: `README.md`
- Create: `FILELIST.md`

**Step 1: Complete Q6-Q10 tests**
測試離線渲染、跨 UUID 相容性與伺服器對話延續性，紀錄完整實驗數據。

**Step 2: Update documentation**
更新 `README.md`, `FILELIST.md`, `CHANGELOG.md` 保持完全同步。
