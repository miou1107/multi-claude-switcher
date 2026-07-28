# Multi-Claude Switcher

在選單列或系統匣一鍵切換多個 Claude Desktop 帳號——不用重新登入，也不會弄丟
Code 的對話紀錄。支援 macOS 與 Windows。

<img src="docs/assets/icon.png" width="96" alt="Multi-Claude Switcher 圖示" align="right" />

[![下載](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=download&style=flat-square)](https://github.com/miou1107/multi-claude-switcher/releases/latest)
[![授權：MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
&nbsp; [English](README.md) | **繁體中文**

**安裝**

- **macOS** — `brew install --cask miou1107/tap/multi-claude-switcher`
  <br>或[下載 app](https://github.com/miou1107/multi-claude-switcher/releases/latest) 拖進「應用程式」（[首次啟動要過一次 Gatekeeper](#macos)）
- **Windows** — [下載安裝程式](https://github.com/miou1107/multi-claude-switcher/releases/latest)並執行。每位使用者安裝，不會跳系統管理員權限。

需要**獨立安裝版**的 [Claude Desktop](https://claude.com/download)。**Microsoft
Store（MSIX）版不支援**——它把資料放在虛擬化的位置，沒辦法替換。App 會自動更新，
所以你只需要裝這一次。

## 這是什麼

- **一鍵切換帳號** — 不用重新登入，也不會弄丟側邊欄的對話。每個帳號各自保持登入狀態。
- **寫入前一定先備份** — 任何會動到 session 資料的操作，都會先做一份加時間戳記的快照；
  萬一備份失敗，寫入會直接中止，而不是覆蓋掉沒被保護的資料。
- **在帳號之間同步 Code session** — 選用功能，預設關閉，而且防衝突：兩邊都改過同一個
  session 時，會保留較新的那份並回報衝突，不會默默覆蓋。
- **找出你已經有的帳號** — Rescan 會掃描這台機器上的 Claude 帳號讓你勾選要管理哪些，
  包含你還沒登入過的那些。

## 切換 vs. 同步

這是兩件不同的事。**除非你打開 Auto Sync，否則單純切換完全不會動到 session 資料。**

- **單純切換（預設）**：點一個帳號，就是關掉 Claude Desktop、用那個帳號重開。沒有任何
  session 資料被搬動，每個帳號保有自己的 Code 紀錄。
- **手動同步**：選一個方向（例如 *工作 → 個人*），把一個帳號的 Code session 複製到另一個，
  **但不改變你現在用的是哪個帳號**。它會關掉 Claude Desktop、備份目標端、複製過去，然後
  把你原本在用的帳號重新打開。
- **切換時自動同步（預設關閉）**：打開之後，每次切換都會把兩邊的 Code session 雙向合併，
  久了兩個帳號的對話紀錄就會一致。因為這等於把一邊的對話併進另一邊，第一次打開時會跳一次
  警告。

切換和同步都會關閉並重開 Claude Desktop——換帳號本來就得這樣才載入得進去。另外，只有
Code 分頁（`claude-code-sessions`）會同步；一般聊天的對話存在 Anthropic 伺服器上、
各帳號獨立，沒辦法在本機同步。

## 安裝

### macOS

```bash
brew install --cask miou1107/tap/multi-claude-switcher
```

或手動安裝：到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)
下載 `Multi-Claude-Switcher_<版本>_macos.zip`，解壓縮後把 **Multi-Claude Switcher.app**
拖進**應用程式**資料夾。

**只有第一次要做：過一次 Gatekeeper。** 這個 app 有 ad-hoc 簽章但沒有公證（公證需要付費的
Apple Developer 帳號），所以 macOS 第一次會要你確認。二選一：

- 對 app 按**右鍵** → **打開**，再在跳出的視窗按**打開**，或
- 如果那個視窗沒有**打開**按鈕（macOS 15 Sequoia 以後）：打開**系統設定 → 隱私權與安全性**，
  往下捲，按**強制打開**。

之後直接雙擊就好。（終端機替代法：`xattr -dr com.apple.quarantine "/Applications/Multi-Claude Switcher.app"`）

### Windows

到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)下載
`Multi-Claude-Switcher_<版本>_windows_setup.exe` 並執行。這是每位使用者安裝（不會跳系統
管理員權限），會建立開始選單捷徑，並在「新增/移除程式」登記一項。然後從**開始選單**啟動
**Multi-Claude Switcher**。

面板需要 **WebView2 Runtime**（微軟的網頁顯示元件）。Windows 11 以及 Windows 10 21H2
以後都已內建；更舊的系統上，app 會跳出對話框附上微軟的安裝連結。

### 移除

- **macOS** — 刪掉 **Multi-Claude Switcher.app**，再刪 `~/.multi-claude-switcher/`
  和 `~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`。
- **Windows** — 從「新增/移除程式」解除安裝，再刪 `%USERPROFILE%\.multi-claude-switcher`。

移除這個 app 不會動到你的 Claude Desktop 帳號或它們的資料。

## 怎麼用

這個 app 沒有 Dock 圖示、也沒有視窗。它待在 **macOS 選單列**（右上角）或
**Windows 系統匣**（右下角，可能要點「顯示隱藏的圖示」那個箭頭），是一對眼睛的圖示。
點它就打開面板。

面板會列出你的帳號和各自的訂閱方案、標示目前使用中的那個；點另一個帳號，確認後就切換過去。
**Rescan**（重新掃描）、**Sync**（同步）、**Rename**（改名）和 **Settings**（設定）全都在
面板裡面。設定裡有 Auto Sync 和**開機自動啟動**兩個開關、手動備份、**檢查更新**、直接打開
log 和備份資料夾的捷徑，以及結束。

點面板以外的地方或按 **Esc** 就關閉。Windows 上再點一次系統匣圖示也會關；對圖示按右鍵有
**Quit**（結束）——萬一面板打不開，那是唯一的出口。

## Team 帳號只能匯出、不能匯入

同步可以**從** Claude Team 帳號**匯出**，但**不能匯入到** Team 帳號。Team 帳號的 Code
對話清單是跟 Anthropic 伺服器拿的，所以複製進它本機資料夾的 session 檔案會被忽略、永遠不會
出現——而且沒有任何設定可以改變這件事。

App 會標示偵測到的 Team 帳號，並在你要做「匯入到它」的動作之前警告你。偵測是盡力而為：
分類不出來的帳號會維持不標記，而不是標錯。

[完整測試結果與原理 →](docs/team-accounts.md)

## 疑難排解

**沒有東西可以切換／只列出一個帳號。** 打開面板 → **Rescan**，勾選你要管理的帳號。你還沒
登入過的帳號也會列出來——先選起來、切換過去，再登入。

**裝的是 Microsoft Store 版。** 請改裝 [claude.com/download](https://claude.com/download)
的獨立安裝版。

**Windows 上面板打不開。** 是缺 WebView2 Runtime——對系統匣圖示按右鍵 → **Quit**，裝好
WebView2 之後再重新啟動。

**出事了，我要把 session 救回來。** 設定 → **Open backup folder**。快照是按設定檔分開、
加時間戳記的。注意快照**不會自動清除**，資料夾會越長越大，手動刪掉舊的是安全的。

**log 在哪？** 設定 → **Open log folder**，或直接找 `~/.multi-claude-switcher/logs/`。

## 參與開發

[從原始碼建置 →](docs/building.md) · [CLI 參考 →](docs/cli.md) ·
[運作原理 →](docs/how-it-works.md)

`FILELIST.md` 說明 repo 裡每個檔案；`CHANGELOG.md` 是版本紀錄。

## 授權

[MIT](LICENSE)。

本專案與 Anthropic 無隸屬關係，也未經其背書。它的做法是讓 Claude Desktop 對著不同的資料
目錄啟動、並在它們之間複製 session 檔案；它不會接觸到你的登入憑證。
