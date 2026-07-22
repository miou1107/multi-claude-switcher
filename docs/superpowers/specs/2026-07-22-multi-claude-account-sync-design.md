# multi-cloude 設計 spec — Claude Desktop 多帳號無縫切換與同步

- 日期：2026-07-22
- 狀態：設計定稿，待實作（Phase 0 探針先行）
- 延續：OwnMind 記憶 #692、#618（Claude Desktop 雙帳號搬家手冊）
- 對抗審查：gpt-5.5（codex）一輪，關鍵意見已納入（見「設計沿革」）

## 一、問題與使用情境

一台電腦上，使用者有多個 Claude Desktop app 帳號（例如公司 Team 帳號 + 個人 Max 訂閱帳號）。某帳號額度用完時要中途切換到另一帳號繼續開發，但兩帳號的對話、記憶、skill、狀態分屬不同 profile，一切換就斷、要重新登入登出、還得重貼背景，嚴重打斷開發節奏。

**目標**：讓使用者額度用完切帳號時：

1. 免登入登出（切過去就是登入好的狀態）
2. 對話、記憶、skill、狀態一致（切過去看到的東西一樣）
3. 操作只要一鍵

**主要情境走查**：用公司 Team 帳號開發 → 額度用完 → 點 tray「切到個人 Max」→ 免登出登入、對話記憶全在 → 繼續開發。

## 二、範圍

- **平台**：Claude Desktop app。第一版只做 **macOS**，Windows 列 backlog（架構預留）。
- **同步範圍**：同一台電腦、多個帳號 profile 之間。**純本機、不上雲、不需登入任何第三方服務。**
- **商業模式**：免費 OSS，發 GitHub。
- **Non-goals（第一版不做）**：
  - 跨電腦同步（symlink 只能同機；跨機要雲端，未來另議）
  - 公司資料外洩防護（使用者明確表示自用情境不列入考量）
  - 即時雙開同步（Claude Desktop 本身不即時刷新 sidebar，見下）

## 三、本機現況（macOS 實機勘查，2026-07-22）

- Claude Desktop 是 **Electron app**。「不同 profile」= 用不同的 `--user-data-dir` 啟動。已知 profile 資料夾：`~/Library/Application Support/Claude`（Profile 1，公司 Team）、`~/Library/Application Support/Claude_Profile2`（個人 Max）。兩個可同時開（使用者已實測）。
- **登入身分**存在各 profile 的 user-data-dir 內：`Cookies`、`buddy-tokens.json`、`config.json`、`Local State`、`ant-did`，以及各類瀏覽器儲存（`IndexedDB`、`Local Storage`、`Session Storage`、`Preferences`、`Network Persistent State`、`Service Worker`）。
- **Desktop 對話索引**（sidebar 來源）存在 `<user-data-dir>/claude-code-sessions/<帳號UUID>/*.json`，每個帳號一個 UUID 桶。
- `~/.claude/`（skill、記憶、commands、CLAUDE.md、對話本體 JSONL 在 `projects/`）**已經全域共用**、跟 Desktop profile 無關。
- **已確認限制**（使用者過往實測）：Desktop app 只在**啟動時**讀 sidebar 對話清單，**不會即時刷新**（另一 process 新增的對話，本 app 不重開看不到）。

## 四、核心設計決策

### 4.1 同步模型：預設「安全切換」，symlink 降為進階實驗

**預設 = 安全切換（Safe Switch）**。切帳號時工具依序做：

1. 偵測並關閉目前正在跑的 Claude profile（若有）
2. 對目標 profile 的 `claude-code-sessions` 做**時間戳快照備份**
3. **同步索引**（把**目標** profile 的對話索引更新成與來源一致，含來源的最新對話；不變量是「兩邊內容相同」；偵測雙邊變更，有衝突讓使用者選，不做靜默合併）
4. 用 `--user-data-dir` 啟動目標 profile

> 為什麼不預設 symlink「永遠一致」：symlink 等於把 Claude Desktop 的**私有檔案格式當成穩定介面在賭**。app 改版重建資料夾、原子寫入 rename、清理暫存檔，都可能讓 symlink 被實體資料夾取代 → 兩邊靜默分叉、使用者不會馬上發現。把 Desktop 索引當「可壞、可重建的快取」處理才穩。
>
> **關鍵：安全切換對使用者仍是一鍵。** 因為切帳號本來就要重開 app（Desktop 不即時刷新），「關掉 → 同步 → 開另一個」對使用者體感一樣無縫，卻多了備份與安全。

**進階實驗選項 = Live Symlink Mode**。明確標「高風險」，預設關閉。啟用條件：同一時間只允許一個 Claude process 使用共享桶，tray 強制做 process 檢查，不符合就拒絕啟動。

### 4.2 共用 vs 獨立的界線

| 資料 | 處理 | 原因 |
|---|---|---|
| `claude-code-sessions/<UUID>/`（Desktop 對話索引） | **同步**（安全切換）/ 可選 symlink | sidebar 顯示來源，要同步的主角 |
| `~/.claude/`（skill、記憶、commands、CLAUDE.md、對話本體） | **已共用、不動** | 本來全域共用、跟帳號無關 |
| 登入身分：`Cookies`、`buddy-tokens.json`、`config.json`、`Local State`、`ant-did` | **獨立、永不同步** | 共用了就變同一帳號，切了等於沒切 |
| 瀏覽器儲存：`IndexedDB`、`Local/Session Storage`、`Preferences`、`Network Persistent State`、`Service Worker` | **獨立、永不同步**（探針證實安全前一律當綁身分） | 身分/帳號狀態可能藏在這裡（mixed state） |
| 快取：`Cache`、`Code Cache`、`GPUCache`、`Crashpad` 等 | **獨立** | 各 profile 各自產生，連了只會互相污染 |

### 4.3 安全機制（貫穿所有操作）

- **每次同步前時間戳快照備份**，所有操作可回滾
- **把 Desktop 索引當可損壞快取**：任何操作都能回滾、重建、比對
- **process 鎖**：偵測到兩 profile 同開同一共享桶 → 阻止寫入或警告停用共享（symlink 模式強制）
- **profile 健康檢查**（每次切換前）：symlink 是否仍存在、目標是否可寫、UUID 是否符合預期、最近備份是否存在、是否有另一 Claude process 正在使用同一共享索引

## 五、架構

```
multi-cloude（Go 專案，跨平台結構）
├─ core/            共用邏輯：切換、同步/reconcile、比差異(diff)、備份/還原、健康檢查
│                   （不碰 OS 細節，全走 platform 介面）
├─ platform/
│   ├─ platform.go  介面定義：找 profile 路徑、啟動(--user-data-dir)、建/拆連結、
│   │               偵測/關閉 process、讀 UUID 桶
│   ├─ darwin.go    macOS 實作（現在做）
│   └─ windows.go   Windows 實作（backlog，介面已留；預設 close-then-sync，不 symlink）
└─ ui/
    ├─ tray/        選單列快速操作（Go systray，跨平台）：
    │               目前 profile、切換、同步狀態、開設定、退出
    └─ settings/    設定視窗（Wails，Go 一份跨平台）：
                    profile 清單、雙邊 diff、衝突處理、備份還原、自動/手動開關、危險操作
```

- **語言：Go**（一份 core 跨平台、單一執行檔；Mac 現在用、Windows 之後重用同一 core）
- **平台差異全鎖在 `platform/` 介面後**：Windows 之後填 windows.go，core 與 UI 不用改
- **UI 分兩塊**：tray 只放少量操作；diff / 衝突 / 備份還原這類複雜操作放設定視窗（tray 選單塞不下）
- **Windows 的真正難點是同步機制不是 GUI**（MSIX 沙箱可能不吃 symlink/junction、`--user-data-dir` 可能被吞、需管理員/開發者模式、檔案鎖更嚴），架構能鋪路但可行性仍需 Windows 實測

## 六、實作階段

### Phase 0：探針（先做，回答成敗關鍵未知）

不寫正式碼，用手動 / 快速腳本回答下列問題。**全程先備份、優先用丟棄式臨時 profile 指向資料複本，不直接動兩個真帳號；測完即刪、不留垃圾。**

**測試/開發環境要求（強制）**：測試與開發必須在**獨立環境**進行 —— 終端機的 Claude Code CLI，**或 Antigravity（或其他非 Claude Desktop 的 IDE）**。**禁止在 Claude Desktop app 的 Code 分頁裡跑**。原因：探針會關閉 / 重開 Claude Desktop、搬動其 profile 資料夾；若開發 session 寄生在 Desktop app 上，會打斷自己、甚至弄壞正在使用的資料（#692 踩過、對應 iron rule 693）。注意：Claude CLI 與 Desktop app 共用 `~/.claude/`，但探針只動 Desktop 的 profile 資料夾（`~/Library/Application Support/Claude*`）、不碰 `~/.claude/`，故 CLI / Antigravity 皆安全。

實測清單（含 codex 對抗審查建議）：

1. Claude Desktop 啟動是否接受任意 `--user-data-dir`，改版後是否仍接受
2. `claude-code-sessions/<UUID>` 是否為 sidebar **唯一**資料來源（還是也讀 IndexedDB / Local Storage / Preferences）
3. symlink / 同步索引後，另一 profile 開 app，sidebar **看不看得到**共用對話（← 核心成敗題）
4. UUID 與登入帳號不一致時，Claude Desktop 會忽略、重建、報錯，還是正常讀取
5. app 是否會在啟動、登出、更新、崩潰恢復時**重建** `claude-code-sessions`（symlink 最陰的死法）
6. macOS symlink 在 app 更新後是否保留
7. sidebar 是讀本機檔案畫出來、還是一定要登入才看得到
8. 同一場對話在兩帳號登入下，是掛同一個 UUID 桶還是各自 UUID
9. 額度用完切帳號繼續同一對話，服務端是否接受同一 session 上下文，還是只是本地 UI 看起來連續
10.（Windows，backlog）MSIX 是否能傳 `--user-data-dir`、能否讀外部 junction/symlink

**產出**：一份「symlink / 安全切換到底可不可行」的明確結論，決定 Phase 1 走向。

### Phase 1：Go core + 安全切換（CLI 先可用）

- `core/` + `platform/darwin.go`：切換、同步索引、備份/還原、diff、健康檢查、process 偵測
- 先做成命令列可用（`switch`、`sync`、`status`、`restore`），使用者可先 dogfood
- 預設安全切換；symlink 為隱藏實驗旗標

### Phase 2：GUI

- tray（Go systray）+ 設定視窗（Wails）
- 自動/手動開關、diff 視圖、備份還原、開機自啟、簽章/公證

## 七、待解未知（Phase 0 要收斂）

- Phase 0 清單全部 10 題
- symlink 若不可行 → 完全走安全切換（關閉後同步），symlink 模式移除
- UUID 桶身分綁定的實際行為（第 4、8 題）會影響「同步」到底是複製哪個桶、還是需要中立 shared UUID

## 八、設計沿革（重要翻案）

- **原方案**（#692）：symlink「永遠一致」當預設。
- **翻案**（本 spec，採納 codex 對抗審查）：改成「安全切換（關閉後同步 + 備份）當預設，symlink 降為進階實驗選項」。理由：symlink 把 app 私有格式當穩定介面在賭，改版易靜默壞；而安全切換對使用者仍是一鍵，體感不變卻更安全。使用者拍板採納。
- **維持**：Go core + 平台轉接層、Mac 先做 Windows backlog、免費 OSS。

## 九、Repo 與命名

- Repo：`/Users/vincentkao/SourceCode/multi-cloude`（獨立 git repo，main 分支）
- 產品名：暫定 `multi-cloude`，發版前可再議
