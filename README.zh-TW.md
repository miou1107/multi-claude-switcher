# Multi-Claude Switcher

<img src="docs/assets/icon.png" width="88" alt="Multi-Claude Switcher 圖示" align="right" />

在同一台電腦上切換多個 Claude Desktop 帳號。免登出、免重打密碼，每個帳號的 Claude Code 對話紀錄各自保留。

[![下載](https://img.shields.io/github/v/release/miou1107/multi-claude-switcher?label=download&style=flat-square)](https://github.com/miou1107/multi-claude-switcher/releases/latest)
[![授權 MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
&nbsp; [English](README.md) | **繁體中文**

---

## 為什麼你需要這個工具？

通常 Claude Desktop 一次只支援使用一個帳號，所以在這兩種情況下你會特別痛：

- **額度用盡**：A 帳號額度用完了，想換 B 帳號繼續寫 Code。
- **公私分明**：公司的專案和自己個人 Side Project，不想全部都混在同一個帳號中。

如果要手動切換帳號的話，你得要先登出再重新登入另一組帳號，而且等它跑完後會發現 **側邊對話紀錄空了**，
因為那些紀錄是跟著上一個帳號走的。

**這個工具就是來解決這件事的：**

1. **一鍵秒切**：點系統匣的常駐圖示可秒切多帳號。
2. **永遠登入**：每個帳號都各自保持登入狀態，切換時完全不用重新輸入帳密。
3. **紀錄不打架**：各帳號的 Code 對話紀錄原封不動、互不干擾。單純切換完全不會
   動到任何 session 資料。
4. **跨帳號對話紀錄同步**：當然你也可以勾選自動同步(或手動同步)，讓所有帳號的對話紀錄保持一致，方便你跨帳號開發。
<img src="docs/assets/panel.png" width="400" alt="面板列出三個帳號：Work 是 Team 方案並標示為目前帳號、Personal 是 Max 20×、Side project 是 Pro，下方是 Rescan 和 Settings 按鈕" />

---

## 安裝

> ⚠️ **Windows用戶重要提醒**
> 你必須使用**獨立安裝版**的 [Claude Desktop](https://claude.com/download)。
> **Microsoft Store 版不支援**：如果你是從 Microsoft Store 安裝 Claude Code，可能會因其沙盒限制而無法正常使用。
>  建議你從 Claude 下載官方 .exe 獨立安裝版。

**macOS**
> macOS使用者推薦使用 brew 指令安裝，快速又方便!
```bash
brew install --cask miou1107/tap/multi-claude-switcher
```

或是到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)
下載 `Multi-Claude-Switcher_<版本>_macos.zip`，解壓縮後把
**Multi-Claude Switcher.app** 拖進**應用程式**。

*只有第一次要做。* 目前 app 沒有Apple Developer 公證簽章，所以安裝時可能會被擋。
請開啟macOS的**系統設定 → 隱私權與安全性**往下捲，按**強制打開**就能完成安裝並正常使用。

或者可以直接用終端機指令來強制開啟，就能正常安裝使用：

```bash
xattr -dr com.apple.quarantine "/Applications/Multi-Claude Switcher.app"
```

**Windows**

到[最新版本](https://github.com/miou1107/multi-claude-switcher/releases/latest)
下載 `Multi-Claude-Switcher_<版本>_windows_setup.exe` 執行。它只裝給你這個使用者，
所以不會跳系統管理員權限。裝完從**開始選單**開 **Multi-Claude Switcher**。


**自動更新**

完成安裝後，只要有新版發布，系統便會自動在背景更新到最新版本。

## 怎麼用

安裝完成後，請在 macOS 選單列或 Windows 系統匣上找尋眼睛圖示，
點擊後功能面板就會跳出來。

面板上會顯示你的帳號清單，並標出你現在在用哪個。可隨時切換到不同帳號，切換後 Claude Desktop
會自動重開至你指定的帳號。切換進行中，面板會在原本確認匡的位置顯示一張進度卡，讓你知道系統正在跑，
而不是懷疑剛剛那下有沒有按到。切好了它會告訴你現在在哪個帳號；萬一切換失敗，它會直接說失敗原因。
同步、合併、備份也是同一張卡片，而且卡片還在的時候面板不會自己關掉。

**Rescan** 會找出這台機器上曾經登入過的帳號，你可以自行勾選並加入帳號清單中。**Change name（改名）**
和 **Remove from list（從清單移除）** 都藏在帳號列上的板手按鈕裡：點一下，一個小選單就會直接
在那一列上打開，不會跳到另一個畫面。

**Change name** 一按下去，那一列本身就會變成一個可以打字的欄位，就地修改。按 Enter 存檔，按 Esc
取消。

**Remove from list** 一樣會打開原本那個確認畫面。按下去會把這個帳號從清單上拿掉，做法是把它的設定檔資料夾
封存起來，不會刪除，你隨時都可以到 Settings 打開這個封存資料夾。之後要再用這個帳號，只要重新
登入一次就好。如果清單上只剩一個帳號，選單裡就不會出現 Remove from list 這一項。如果 Claude 現在正開著
這個帳號，選單裡還是有 Remove from list，只是選下去會告訴你要先切到別的帳號，所以你要移除的是「另一個」
帳號時，完全不需要先關掉 Claude。在 Windows 市集版（Store 版）上，還會拒絕移除：目前占用那唯一
共用槽位的帳號、MCS 還沒掃描過槽位狀態的安裝，以及對話還在排隊要搬進剛加入之新帳號的那個帳號。
順利移除完會直接回到帳號清單，上面用一行字告訴你完成了；移除失敗，或是移除成功但留下了沒清乾淨
的東西，才會另外看到一個說明原因的畫面。

**Settings** 裡有自動同步對話紀錄及開機自行啟動兩個開關，並且有手動備份、檢查更新、查看 log、
備份資料夾和封存資料夾等額外功能。

按 **Esc** 或點面板外面就關掉。Windows 上再點一次系統匣圖示也會關，對圖示按右鍵有
**Quit**，萬一面板本身開不起來，那是你的逃生門。

## 把對話搬到另一個帳號

切換帳號和對話紀錄同步是兩個不同的功能。**單純切換完全不會動到 session 資料。** 如果你只是要在帳號之間
跳來跳去，不需要讓對話紀錄在兩個帳號間同步，那這一段可以直接跳過。

真的想讓對話跟著你走的話，有兩條路：

**Sync sessions** 是把一個帳號的 Code session 複製到另一個，但不改變你現在用的是哪個
帳號。它會先備份目標端，弄完再把你原本在用的帳號開回來。

**切換時自動同步**預設是關的。打開之後，每次切換都會把兩邊的 session 雙向合併，久了
兩個帳號的紀錄就會一致。第一次打開會警告你一次，因為合併過去的東西，不會因為你把開關
關掉就退回來。

不管走哪條，只要兩邊都有同一筆對話而狀態不同，會保留較晚被更新的那份，另一份會回報給你，
而不是默默丟掉。而且每次寫入前都會先做一份加時間戳記的快照；快照失敗的話，這次寫入就直接
不做。

同步的只有 Code 分頁，而且只有對話**清單**。一般聊天的對話存在 Anthropic 伺服器上、每個
帳號各一份，本機做什麼都搬不動它。而 Code 的對話內容又存在另一個地方，Claude Code 會依
它自己的保留期自動清掉舊的，所以清單上可能有一筆對話、點進去卻沒有內容了。那是 Claude
自己的例行整理，這個工具既不會造成它、也沒辦法把它救回來。

## Team 帳號和其他帳號一樣可以同步

這份文件之前寫「Team 帳號的對話只能匯出、不能匯入」。那是錯的，而且錯在這個工具本身。

對話在硬碟上是照「帳號」加「組織」兩層歸檔的。同步時只把帳號那層換成目標帳號，組織那層
還留著來源帳號的組織，等於檔案完整地放進了目標帳號永遠不會去讀的那一格。看起來就跟
「Team 帳號拒絕匯入」一模一樣，當初也就這樣誤判了。

現在兩層都會換，所以匯入 Team 帳號的對話會落在該帳號真的會讀的位置。Team 帳號不需要任何
特殊處理，同步前那些針對 Team 帳號的警告也一併移除了。

[想看修正前後分別實測了什麼 →](docs/team-accounts.md)

## 回報問題

Settings → **Debug info** 會列出這個切換工具知道的機器資訊：版本號、每個帳號在硬碟上
長什麼樣子、還有每個 log 檔案最後的內容。按 **Report a problem** 會把這份報告複製到
剪貼簿，並開啟一個預先填好的 GitHub issue 頁面讓你貼上去。

沒有任何東西會自動送出。你會先看到完整報告，貼上去、送出去都是你自己的帳號動手做的。
Email、帳號 ID、你的使用者名稱和家目錄路徑，在報告顯示出來之前就已經換成像
`account-1`、`org-A` 這樣的代稱，彼此的對應關係還看得出來，但真正的值不會離開你的電腦。
換完代稱之後還會再跑一次以外觀特徵抓漏的檢查，把任何看起來像 email 或帳號 ID、卻沒被
換掉的東西直接塗黑，避免漏掉沒登記到的欄位。Issue 是公開的。

## 常見問題與救援

**Q：清單上為什麼只看到一個帳號？**
打開面板點 **Rescan**，勾選你要管理的帳號。還沒登入過的帳號也會列出來：先勾選它，
切換過去，再在 Claude Desktop 裡完成登入。

**Q：Rescan 出現「Signed out in Claude Desktop」而且勾不動？**
那個帳號是在 Claude Desktop 裡登出的，這會蓋掉該資料夾唯一的一組登入資料。它的對話
其實都還在硬碟上。點 **Recover**，替它取個名字，在跳出來的 Claude 視窗裡登入一次就好，
對話會自己跟回來。

要避免這種狀況，加帳號請用面板裡的 **＋ Add another account**，不要在 Claude Desktop
裡登出。這樣每個帳號一開始就有自己獨立的設定檔。

**Q：（Windows）面板打不開怎麼辦？**
通常是你的電腦缺了 WebView2 套件。對系統匣的眼睛圖示按右鍵選 **Quit**，請自行安裝好 WebView2 之後再重開 (理論上 Win10 21H2 版本都有內建)。

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
請自行判斷、斟酌使用本專案程式碼，作者不提供任何承諾與保障，任何風險及後果由使用者自行承擔。
