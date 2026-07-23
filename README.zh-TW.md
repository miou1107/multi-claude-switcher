# multi-claude-switcher

[English](README.md) | **繁體中文**

<img src="docs/assets/icon.png" width="120" alt="Multi-Claude Switcher 圖示" />

<img width="236" height="277" alt="image" src="https://github.com/user-attachments/assets/62f863dd-9545-4a5c-ac46-66c32517f21f" />

<img width="2457" height="829" alt="image" src="https://github.com/user-attachments/assets/fa5eef07-356a-4f8a-8eba-4f82d8e9f531" />

在 Claude Desktop 上無縫切換與同步多個帳號（macOS 與 Windows）。

> ### ⚠️ 先看這個：Claude **Team** 帳號只能匯出、不能匯入
> 對話同步可以**從 Team 帳號匯出**（Team → 個人 ✅），但**無法匯入 Team 帳號**（任何帳號 → Team ❌）。Team 帳號的 Code 對話清單是**向 Anthropic 伺服器抓取**的（鎖定你的帳號＋組織），所以複製進它本地資料夾的 session 檔會被忽略、永遠不會出現，連乾淨重啟都一樣。**你無法用同步檔案的方式，把個人帳號的 Code 對話帶進公司 Team 帳號。** [詳細說明與證據見下方](#-在帳號之間同步-session)。

## 📌 功能特色

- **安全切換（Safe Switch）**：在多個 Claude Desktop 設定檔（`~/Library/Application Support/Claude*`）之間切換，不用重新登入，也不會弄丟側邊欄的對話紀錄。
- **自動備份**：在任何會寫入 session 的動作之前（手動對齊、`mcs sync`、或開了 Auto Sync 的切換），都會先對 `claude-code-sessions` 做一份加上時間戳記的快照。單純的切換（預設，Auto Sync 關閉）不會動到 session 資料，所以不會備份。萬一備份失敗，寫入會中止，而不是把沒被保護的資料覆蓋掉。
- **防衝突同步**：當兩個設定檔都改過同一個 session 時，會保留較新的那一份（目標端），並回報為衝突，而不是默默覆蓋。
- **探測驗證工具**：內含 `scripts/probe/probe_runner.py`，用來檢視設定檔、驗證本機 session 同步。

## 🔄 在帳號之間同步 session

切換帳號和同步 session 是兩件不同的事，單純切換除非你開了 auto sync，否則不會動到 session 資料。

- **單純切換（預設）：** 在選單點一個設定檔，只會關掉 Claude Desktop 再用那個設定檔重開。不會搬動任何 session 資料，每個帳號只保有自己的 Code 對話紀錄。
- **手動對齊，「Sync sessions」子選單：** 選一個方向（例如 `From Company → To Personal`），把一個帳號的 Code sessions 複製到另一個帳號，**不會切換你目前所在的帳號**。它會關掉 Claude Desktop、備份目標帳號、把 sessions 複製過去，再用你原本正在用的帳號重開。
- **「Auto Sync on Switch」開關（預設關閉）：** 打開後，每次切換都會把兩個帳號的 Code sessions 做雙向聯集，於是兩個帳號的對話紀錄會隨時間收斂成一致。因為打開它會把一個帳號的對話併進另一個，所以啟用時會跳一次性的警告視窗（附「Enable, don't ask again」選項可略過日後的警告）。

> **範圍：** 只有 Code 分頁（`claude-code-sessions`）會同步。一般聊天的對話存在各帳號的伺服器端，無法在本機同步。Agent Mode / Cowork 的 session 目前尚未涵蓋。
>
> **⚠️ Claude Team 帳號只能「匯出」，不能「匯入」。** 2026-07-23 直接實測：
>
> - ✅ **Team → 個人（匯出）有效。** 把 Team 帳號的 Code 對話複製進個人帳號的資料夾，個人帳號會顯示。
> - ❌ **任何帳號 → Team（匯入）無效。** 把別的帳號的 session 檔複製進 Team 帳號的資料夾**完全沒用**，對話不會出現在 Team 帳號的側邊欄，連乾淨重啟、整個清快取重建都一樣。
>
> 原因：Claude Desktop 建 Team 帳號的 Code 側邊欄清單,是**向 Anthropic 伺服器抓取**的,範圍鎖定你的帳號**與組織**（app 帶 `orgUuid` 呼叫 `sessions_api_list_sessions`;官方文件也說 session 對話記錄存在伺服器）。對 Team 帳號來說**伺服器才是真相來源**,複製進去的本地檔案會被忽略,也**沒有設定可以切成讀本地**。結論：**你無法用同步檔案的方式,把個人帳號的 Code 對話匯入公司 Team 帳號。** 只有 app 把本地 `claude-code-sessions/` 當真相來源的情況（個人帳號）匯入才有效。詳見 `docs/superpowers/specs/2026-07-22-probe-results.md`。
>
> tray 會把偵測到的 Team 帳號標上 `🏢 Team`,並在你做「會匯入它」的動作時提醒(開 Auto Sync、或 Sync 方向指向它)。偵測是盡力而為 ── 判不出來的帳號會維持不標,不會亂標。

## 📥 下載

[![下載最新版](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=Download%20app&style=for-the-badge)](https://github.com/miou1107/multi-claude-switcher/releases/latest)

到[最新 release](https://github.com/miou1107/multi-claude-switcher/releases/latest)下載你平台對應的壓縮包：

> **macOS — `Multi-Claude-Switcher_<版本>_macos.zip`** — 解壓即用的
> **Multi-Claude Switcher.app**（通用版 macOS 應用程式，Apple Silicon +
> Intel 都可）。解壓後直接執行，不用建置或編譯。
>
> **Windows — `Multi-Claude-Switcher_<版本>_windows_setup.exe`** — 安裝程式
> （每位使用者、不需系統管理員權限）；執行它、再從開始選單啟動。

### macOS

1. 下載上面的 `Multi-Claude-Switcher_<版本>_macos.zip` 並**解壓**（雙擊壓縮檔）。你會得到 **Multi-Claude Switcher.app**。
2. 把 **Multi-Claude Switcher.app** 拖到你的**應用程式（Applications）**資料夾。
3. **只有第一次啟動，通過一次 Gatekeeper。** 這個 app 有 ad-hoc 簽章、但未經 Apple 公證（公證需要付費的 Apple 開發者帳號），所以 macOS 第一次會要你確認。二選一：
   - 對 app 按**右鍵** →**打開**，再在跳出的視窗按**打開**，或
   - 如果視窗沒有 **打開** 按鈕（macOS 15 Sequoia 以後）：打開**系統設定 → 隱私權與安全性**，往下捲，按**強制打開（Open Anyway）**。

   之後直接雙擊即可。（終端機替代法：`xattr -dr com.apple.quarantine "/Applications/Multi-Claude Switcher.app"`。）

這個 app 跑在**選單列**（右上角），顯示為一對眼睛的圖示，沒有 Dock 圖示。點它打開選單，每個帳號是一個子選單，裡面有 **Switch to this profile**（切換到這個 profile）和 **Rename…**（改名），目前使用中的帳號會標記出來。從選單開啟 **Start at Login** 可讓它開機自動啟動。App 會**自我更新**（從 GitHub Releases 抓），所以只要裝一次。

### Windows

1. 下載上面的 **`Multi-Claude-Switcher_<版本>_windows_setup.exe`** 並執行。這是「每位使用者」安裝（不會跳系統管理員權限），會裝好程式、建立開始選單捷徑，並在「新增/移除程式」登記一項。
2. 從**開始選單**啟動 **Multi-Claude Switcher**。它會出現在系統匣（右下角，可能要點「顯示隱藏的圖示」那個箭頭）成為一對眼睛的圖示。點它打開選單，每個帳號是一個子選單，裡面有 **Switch to this profile**（切換到這個 profile）和 **Rename…**（改名），目前使用中的帳號會標記出來。從選單開啟 **Start at Login** 可讓它開機自動啟動。有新版本時它會通知你；點 **Check for Updates** 會打開下載頁，執行新的安裝程式即可升級（會直接覆蓋舊版）。

> **需要獨立安裝版的 Claude Desktop。** 請到 [claude.com/download](https://claude.com/download) 安裝 Claude Desktop（一般的個人安裝版）。**Microsoft Store / MSIX** 版**目前尚未支援**：它把資料存在一個虛擬化的位置，而且沒辦法用自訂的設定檔目錄重新啟動，而切換正是靠這個機制運作的。如果你裝的是商店版，請改裝獨立安裝版才能使用本工具。

> **同步為何不會出錯**：Code 分頁只會列出「以該設定檔自己登入帳號命名」的那個 bucket 裡的對話。同步會讀取來源設定檔的帳號 bucket，把那些 sessions 重新歸到**目標**設定檔的帳號 bucket 底下，所以跨帳號切換後對話能正確出現（已在裝置上驗證），而不是默默把 sessions 丟進目標 app 根本不會讀的 bucket。

## 📁 專案結構

```
multi-claude-switcher/
├── docs/
│   ├── assets/
│   │   └── icon.png                   # README / 文件用的 app 圖示
│   ├── plans/                         # 實作計畫
│   │   └── 2026-07-22-phase-0-probe.md
│   └── superpowers/
│       └── specs/                     # 設計規格與探測報告
│           ├── 2026-07-22-multi-claude-account-sync-design.md
│           └── 2026-07-22-probe-results.md
├── scripts/
│   ├── gen-icons/
│   │   └── main.go                    # 由幾何生成所有圖示素材
│   └── probe/
│       └── probe_runner.py            # Phase 0 探測工具
├── CHANGELOG.md                       # 版本歷史
├── FILELIST.md                        # 專案檔案清單
└── README.md                          # 專案說明文件
```

## 🚀 快速開始

### 建置執行檔

```bash
# 建置 CLI 工具
go build -o bin/mcs ./cmd/mcs

# 建置系統匣 GUI app
go build -o bin/mcs-tray ./cmd/mcs-tray

# 打包成可雙擊的 macOS .app（通用版，輸出到 dist/）
./scripts/package-app.sh 0.6.0
```

在 **Windows**（PowerShell）建置工具列程式 + CLI，純 Go、不需要 CGO / C 工具鏈：

```powershell
go build -o bin/mcs-tray.exe ./cmd/mcs-tray
go build -o bin/mcs.exe ./cmd/mcs
```

### 啟動系統匣 App

```bash
./bin/mcs-tray
```

在 macOS 選單列以一對眼睛的圖示出現，一鍵切換設定檔與備份。圖示會標示目前正在用的設定檔，App 也會自動去 GitHub 檢查並安裝更新。

### CLI 指令

查看偵測到的設定檔與執行中的程序：

```bash
./bin/mcs status
```

備份所有設定檔的 session 索引：

```bash
./bin/mcs backup
```

把 session 檔從來源設定檔同步到目標設定檔：

```bash
./bin/mcs sync Claude Claude_Profile2
```

執行安全切換（關掉執行中的 app → 備份 → 同步 → 啟動目標設定檔）：

```bash
./bin/mcs switch Claude Claude_Profile2
```

從備份快照還原 session 索引：

```bash
./bin/mcs restore ~/.multi-claude-switcher/backups/Claude_20260722_103206 Claude
```

## 📜 授權

MIT License。
