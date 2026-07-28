# Multi-Claude Switcher

<img src="docs/assets/icon.png" width="88" alt="Multi-Claude Switcher 圖示" align="right" />

在同一台電腦上切換多個 Claude Desktop 帳號。免登出、免重打密碼，每個帳號的
Code 對話紀錄各自保留。

[![下載](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=download&style=flat-square)](https://github.com/miou1107/multi-claude-switcher/releases/latest)
[![授權 MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
&nbsp; [English](README.md) | **繁體中文**

---

## 為什麼你需要這個工具？

Claude Desktop 一次只記得一個帳號。這兩種情況下特別痛：

- **額度用盡**：A 帳號用量撞牆了，想換 B 帳號繼續寫 Code。
- **公私分明**：公司的 Team 專案和自己的 Side Project，不想全部塞在同一個帳號裡。

手動切換的話，你得登出、重新登入、等它跑完，然後發現 **Code 側邊欄空了**，
因為那些紀錄是跟著上一個帳號走的。麻煩到最後，多數人乾脆不換了。

**這個工具就是來解決這件事的：**

1. **一鍵秒切**：點系統匣圖示、選帳號、確認，Claude Desktop 就用那個帳號重開。
2. **永遠登入**：每個帳號各自保持登入狀態，切換時完全不用重新輸入帳密。
3. **紀錄不打架**：各帳號的 Code 對話紀錄原封不動、互不干擾。單純切換完全不會
   動到任何 session 資料。

<img src="docs/assets/panel.png" width="400" alt="面板列出三個帳號：Work 是 Team 方案並標示為目前帳號、Personal 是 Max 20×、Side project 是 Pro，下方是 Rescan 和 Settings 按鈕" />

---

## 安裝

> ⚠️ **重要前提**
> 你必須使用**獨立安裝版**的 [Claude Desktop](https://claude.com/download)。
> **Microsoft Store 版不支援**：它的資料放在虛擬化的位置換不掉，而整個工具就是
> 靠換那個位置在運作的。

**macOS**

```bash
brew install --cask miou1107/tap/multi-claude-switcher
```

或是到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)
下載 `Multi-Claude-Switcher_<版本>_macos.zip`，解壓縮後把
**Multi-Claude Switcher.app** 拖進**應用程式**。

*只有第一次要做。* 這個 app 有 ad-hoc 簽章但沒有公證，因為公證要付費的 Apple
Developer 帳號，所以 macOS 第一次會擋一下。對 app 按右鍵選**打開**，在跳出來的
視窗再按一次**打開**就好。如果那個視窗沒有**打開**按鈕（macOS 15 Sequoia 以後
改了），就去**系統設定 → 隱私權與安全性**往下捲，按**強制打開**。之後就跟一般
app 一樣雙擊。習慣用終端機的話：

```bash
xattr -dr com.apple.quarantine "/Applications/Multi-Claude Switcher.app"
```

**Windows**

到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)
下載 `Multi-Claude-Switcher_<版本>_windows_setup.exe` 執行。它只裝給你這個使用者，
所以不會跳系統管理員權限。裝完從**開始選單**開 **Multi-Claude Switcher**。

面板是用 **WebView2 Runtime** 畫的，Windows 11 和 Windows 10 21H2 以後都內建了。
更舊的系統上 app 會直接給你微軟的安裝連結。

更新會自己在背景裝好，兩個平台都是。你只需要裝這一次。

## 怎麼用

它沒有視窗、也沒有 Dock 圖示。就是 macOS 選單列或 Windows 系統匣上一個眼睛的圖示，
點下去面板就出來。

面板列出你的帳號和各自的方案，標出你現在在用哪個。點另一個、確認，Claude Desktop
就用它重開。這個重開躲不掉：Claude Desktop 只在啟動時讀一次帳號資料，沒辦法讓一個
跑著的 app 中途換帳號。

**Rescan** 會找出這台機器上已經有的帳號，包含你還沒登入過的。**Rename** 讓你把
設定檔改成看得懂的名字。**Settings** 裡有兩個開關、手動備份、檢查更新、直接開啟
log 和備份資料夾的捷徑，還有結束。

按 **Esc** 或點面板外面就關掉。Windows 上再點一次系統匣圖示也會關，對圖示按右鍵有
**Quit**——萬一面板本身開不起來，那是你的逃生門。

## 把對話搬到另一個帳號

切換和同步是兩件事。**單純切換完全不會動到 session 資料。** 如果你只是要在帳號之間
跳來跳去，這一段可以直接跳過。

真的想讓對話跟著你走的話，有兩條路：

**Sync sessions** 是把一個帳號的 Code session 複製到另一個，但不改變你現在用的是哪個
帳號。它會先備份目標端，弄完再把你原本在用的帳號開回來。

**切換時自動同步**預設是關的。打開之後，每次切換都會把兩邊的 session 雙向合併，久了
兩個帳號的紀錄就會一致。第一次打開會警告你一次，因為合併過去的東西，不會因為你把開關
關掉就退回來。

不管走哪條，只要兩邊都改過同一個 session，會保留比較新的那份，並且把衝突報給你，而不是
自己默默決定。而且每次寫入前都會先做一份加時間戳記的快照；快照失敗的話，這次寫入就直接
不做。

只有 Code 分頁會同步。一般聊天的對話存在 Anthropic 伺服器上、每個帳號各一份，本機做
什麼都搬不動它。

## Team 帳號只出不進

你可以把 Code session **從** Claude Team 帳號複製**出來**，但**複製不進去**。Team 帳號
的對話清單是跟 Anthropic 伺服器要的，所以你放進它本機資料夾的檔案永遠不會被讀取，而且
沒有任何設定改得了這件事。

App 會標示它認得出來的 Team 帳號，並且在你要做「匯入到它」的動作之前先警告。這個判斷是
盡力而為：認不出來的帳號會維持不標記，而不是標錯。

[實際測了什麼、為什麼會這樣 →](docs/team-accounts.md)

## 常見問題與救援

**Q：清單上為什麼只看到一個帳號？**
打開面板點 **Rescan**，勾選你要管理的帳號。還沒登入過的帳號也會列出來：先勾選它，
切換過去，再在 Claude Desktop 裡完成登入。

**Q：（Windows）面板打不開怎麼辦？**
通常是缺 WebView2。對系統匣的眼睛圖示按右鍵選 **Quit**，裝好 WebView2 之後再重開。

**Q：我的對話紀錄亂掉了，救得回來嗎？**
可以。Settings → **Open backup folder**，裡面是按設定檔分開、加了時間戳記的快照。
注意快照**不會自動清理**，太佔空間的話手動刪掉舊的是安全的。

**Q：log 在哪裡？**
Settings → **Open log folder**，或直接找 `~/.multi-claude-switcher/logs/`。

**Q：如何徹底移除？**

- **macOS**：刪掉 app，再清掉 `~/.multi-claude-switcher/` 和
  `~/Library/LaunchAgents/com.miou1107.multi-claude-switcher.plist`。
- **Windows**：從「新增/移除程式」解除安裝，再刪掉 `%USERPROFILE%\.multi-claude-switcher`。

移除本工具完全不會影響你原本的 Claude Desktop 帳號與資料。

---

## 參與開發與授權

[從原始碼建置 →](docs/building.md) ·
[CLI 參考 →](docs/cli.md) ·
[運作原理 →](docs/how-it-works.md)

`FILELIST.md` 說明 repo 裡的每個檔案，`CHANGELOG.md` 是版本紀錄。

授權條款：[MIT](LICENSE)。

**免責聲明**：本專案與 Anthropic 無任何隸屬關係。工具的運作原理只是讓 Claude Desktop
對著不同的本機資料目錄啟動，並在它們之間複製 session 檔案；它不會碰到你的登入憑證，
更不會上傳任何東西。
