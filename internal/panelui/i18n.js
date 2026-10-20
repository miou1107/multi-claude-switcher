// Presentation-only localization of the panel. Never changes form values,
// account names, data attributes, action names, or diagnostic report contents.
//
// The host writes the chosen language into window.__mcsLang before this file
// runs. "auto" follows the computer's own preferred language, which is what
// the panel did before the setting existed; the three explicit codes override
// it. Any other value leaves the panel in English rather than guessing.
(() => {
  'use strict';

  const DICTS = {
    "zh-Hant": {
      "Language": "語言",
      "Which language the panel is shown in": "面板上的文字要用哪一種語言",
      "Automatic": "自動",
      "Multi-Claude Switcher": "Claude 帳號切換器",
      "What's new": "版本更新說明",
      "Close": "關閉",
      "Close Claude?": "要關閉 Claude 嗎？",
      "Anything unsaved in Claude is interrupted.": "注意！Claude 中未存檔的工作將會中斷",
      "Cancel": "取消",
      "Continue": "繼續",
      "Switch": "切換",
      "Sync": "同步",
      "Merge": "合併",
      "Remove": "移除",
      "Got it": "知道了",
      "Remove from list": "從清單移除",
      "The removal failed, and no reason was given. Try again.": "移除失敗，查不到原因。請再試一次。",
      "Account options": "帳號選項",
      "Change name": "變更名稱",
      "Save": "儲存",
      "Duplicate": "重複帳號",
      "In use now": "使用中",
      "Not signed in yet. Switch here, then sign in.": "還沒登入。切換過來之後再登入。",
      "No managed accounts yet. Run Rescan to add some.": "還沒有加入任何帳號，按「重新掃描」加入。",
      "Add another account": "新增其他帳號",
      "Add": "新增",
      "Switch or manage your Claude accounts": "切換與管理你的 Claude 帳號",
      "Rescan": "重新掃描",
      "Settings": "設定",
      "Check for updates": "檢查更新",
      "Preferences and maintenance": "偏好設定與維護",
      "Auto Sync on switch": "切換時自動同步",
      "Merge both accounts' Code sessions when switching": "切換時把兩個帳號的 Code 對話合併",
      "Start at login": "登入時自動啟動",
      "Launch Multi-Claude Switcher when you log in": "登入之後自動啟動切換器",
      "More": "更多",
      "Quit Multi-Claude Switcher": "結束切換器",
      "Report a bug": "回報問題",
      "Back up all accounts": "備份所有帳號",
      "Sync, backups, and where things are kept": "同步、備份與檔案位置",
      "Sync between accounts": "帳號之間同步",
      "Open backup folder": "開啟備份資料夾",
      "Open archive folder": "開啟封存資料夾",
      "Exactly what a report contains": "問題回報的內容有哪些",
      "Your email address, account IDs, user name and home folder are replaced with stand-ins like account-1 below. A name you gave a profile folder yourself can still show. [redacted] marks something that looked like an address or an ID and was blocked.": "下面已經把電子郵件、帳號 ID、使用者名稱與個人資料夾路徑換成 account-1 這類代號。你自己填過的資料夾名稱還是會出現。報告裡顯示成 [redacted] 的地方，原本是疑似電子郵件或帳號 ID 的內容，已經遮蔽。",
      "What went wrong? (optional)": "遇到什麼問題？（選填）",
      "Switching to my work account left the personal one closed…": "例如：切換到工作帳號之後，個人帳號沒有重新開啟……",
      "Copy": "複製",
      "Report a problem": "回報問題",
      "Open a GitHub issue?": "要開啟 GitHub 問題回報頁嗎？",
      "Copy and open": "複製並開啟",
      "The report above and your comment are copied to your clipboard, and your browser opens a new issue on the MCS repository. Paste it there and you can still edit it before submitting.": "上面的報告與你寫的說明會複製到剪貼簿，瀏覽器會開啟這個專案的問題回報頁。貼上之後還可以修改，最後由你送出。",
      "GitHub issues are public. What is copied is exactly what you saw on the screen behind this dialog, with your email address, account IDs, user name and home folder already replaced with stand-ins.": "GitHub 的問題回報是公開的。複製的內容就是剛才看到的報告，其中電子郵件、帳號 ID、使用者名稱與個人資料夾路徑都已經換成代號。",
      "No date yet": "還沒有日期",
      "Unrecognized account": "無法辨識的帳號",
      "Signed out in Claude Desktop": "已在 Claude 桌面版登出",
      "Recover": "復原",
      "Not signed in yet. Select it here, then switch to it and sign in.": "還沒登入。先在這裡勾選，再切換過去登入。",
      "Its conversations are still here. Recover to sign back in.": "對話都還在。按「復原」就可以重新登入。",
      "Sign in to finish setting this up.": "登入之後設定就完成了。",
      "Invalid account data": "帳號資料無效",
      "Personal": "個人",
      "Free": "免費版",
      "Choose accounts": "選擇帳號",
      "Which accounts should be managed?": "要管理哪些帳號？",
      "Confirm": "確認",
      "It gets its own profile": "會建立自己的帳號環境",
      "Recover this account": "復原這個帳號",
      "different account": "另一個帳號",
      "Sign in as a": "請登入",
      ". Signing in as one you already have creates a duplicate, and MCS will ask you to merge.": "。如果登入已經加入過的帳號，就會多出一份重複的環境，切換器會提示你合併。",
      "Sign in as the account starting": "請登入帳號 ID 開頭是這樣的帳號：",
      "Claude closes, your current account is saved, and a clean Claude opens.": "Claude 會關閉，目前的帳號環境會存起來，然後開啟一個全新的 Claude。",
      "Will be archived": "會封存這份環境",
      "Keep this one": "保留這份環境",
      "In use now · keep this one": "使用中 · 保留這份環境",
      "Merge duplicates": "合併重複的環境",
      "Both are the same account": "這兩份環境是同一個帳號",
      "Add at least two managed accounts to sync between them.": "至少要加入兩個帳號，才能在帳號之間同步。",
      "Claude closes, then reopens where you were": "Claude 會關閉，完成後回到原本的帳號",
      "Nothing was moved": "沒有搬動任何檔案",
      "The account is still on your list, so you can try again.": "帳號還在清單上，可以再試一次。",
      "Back": "返回",
      "Try again": "再試一次",
      "Its folder is in your archive, not deleted.": "資料夾已經封存，檔案還在。",
      "It is off the switcher": "已經從切換器移除",
      "Done": "完成",
      "Switch to another account first, then you can remove it.": "請先切換到另一個帳號，才能移除這一個。",
      "It comes off your list. Nothing is deleted, and you can sign in to it again any time.": "這個帳號會從清單移除，檔案不會刪除，之後隨時可以重新登入。",
      "Switching profile": "正在切換帳號",
      "Claude will restart in a moment.": "Claude 馬上會重新啟動。",
      "Switch failed": "切換失敗",
      "Switched successfully": "切換完成",
      "Syncing conversations": "正在同步對話",
      "Claude closes while this runs, and reopens when it is done.": "執行期間 Claude 會關閉，完成後重新開啟。",
      "Sync failed": "同步失敗",
      "Sync finished": "同步完成",
      "Sync finished.": "同步完成。",
      "Quit Claude Desktop first, then try Sync again.": "請先結束 Claude 桌面版，再重新同步一次。",
      "Merging accounts": "正在合併帳號環境",
      "Merge failed": "合併失敗",
      "Accounts merged": "帳號環境已合併",
      "The two accounts are now one.": "同一個帳號的兩份環境已經合併成一份。",
      "Adding the account": "正在新增帳號",
      "Recovering the account": "正在復原帳號",
      "Account added": "帳號已新增",
      "Account recovered": "帳號已復原",
      "Backing up accounts": "正在備份帳號",
      "Backup failed": "備份失敗",
      "Backup finished": "備份完成",
      "Nothing to back up": "沒有需要備份的內容",
      "No account had any conversations stored yet.": "這些帳號都還沒有存下任何對話。",
      "It did not finish.": "沒有完成。",
      "not signed in": "未登入",
      "Finishing up, then quitting…": "正在收尾，馬上結束……",
      "Busy right now. Try the rename again in a moment.": "現在有其他工作在跑，請稍後再改名稱。",
      "That account is no longer recoverable. Run Rescan": "這個帳號已經無法復原，請重新掃描。",
      "That account is no longer there. Run Rescan": "這個帳號已經不在了，請重新掃描。",
      "that account is no longer there. Run Rescan": "這個帳號已經不在了，請重新掃描。",
      "one of those profiles is no longer there. Run Rescan": "其中一份帳號環境已經不在了，請重新掃描。",
      "No account was named": "請填寫帳號名稱。",
      "Setting up…": "正在設定……",
      "Removing…": "正在移除……",
      "Gathering debug info…": "正在收集診斷資訊……",
      "Copying report…": "正在複製報告……",
      "Report copied. Paste it into the issue.": "報告已複製，貼到問題回報頁即可。",
      "Copying…": "正在複製……",
      "Copied.": "已複製。",
      "Checking for updates…": "正在檢查更新……",
      "Checking for updates… the answer will pop up in a moment.": "正在檢查更新……結果稍後會顯示。",
      "An update is already in progress.": "已經在更新了，請稍候。",
      "Update check failed. Try again later.": "檢查更新失敗，請稍後再試。",
      "Some conversations were skipped": "有部分對話已跳過",
      "Recovered": "已復原",
    },
    "zh-Hans": {
      "Language": "语言",
      "Which language the panel is shown in": "面板上的文字用哪一种语言",
      "Automatic": "自动",
      "Multi-Claude Switcher": "Claude 多账号切换器",
      "What's new": "版本更新说明",
      "Close": "关闭",
      "Close Claude?": "关闭 Claude？",
      "Anything unsaved in Claude is interrupted.": "Claude 中尚未完成或保存的操作会被中断。",
      "Cancel": "取消",
      "Continue": "继续",
      "Switch": "切换",
      "Sync": "同步",
      "Merge": "合并",
      "Remove": "移除",
      "Got it": "知道了",
      "Remove from list": "从列表移除",
      "The removal failed, and no reason was given. Try again.": "移除失败，查不到原因。请重试。",
      "Account options": "账号选项",
      "Change name": "修改名称",
      "Save": "保存",
      "Duplicate": "重复账号",
      "In use now": "当前使用中",
      "Not signed in yet. Switch here, then sign in.": "尚未登录。切换到此账号后再登录。",
      "No managed accounts yet. Run Rescan to add some.": "尚未添加账号。点击“重新扫描”添加。",
      "Add another account": "添加其他账号",
      "Add": "添加",
      "Switch or manage your Claude accounts": "切换和管理你的 Claude 账号",
      "Rescan": "重新扫描",
      "Settings": "设置",
      "Check for updates": "检查更新",
      "Preferences and maintenance": "偏好设置与维护",
      "Auto Sync on switch": "切换时自动同步",
      "Merge both accounts' Code sessions when switching": "切换时双向合并两个账号的 Code 会话索引",
      "Start at login": "开机登录时启动",
      "Launch Multi-Claude Switcher when you log in": "登录 Mac 时自动启动此切换器",
      "More": "更多功能",
      "Quit Multi-Claude Switcher": "退出切换器",
      "Report a bug": "反馈问题",
      "Back up all accounts": "备份全部账号",
      "Sync, backups, and where things are kept": "同步、备份与存储位置",
      "Sync between accounts": "账号间同步",
      "Open backup folder": "打开备份文件夹",
      "Open archive folder": "打开归档文件夹",
      "Exactly what a report contains": "查看反馈报告的具体内容",
      "Your email address, account IDs, user name and home folder are replaced with stand-ins like account-1 below. A name you gave a profile folder yourself can still show. [redacted] marks something that looked like an address or an ID and was blocked.": "下面已用 account-1 等代号替换邮箱、账号 ID、用户名和个人文件夹路径。你自行填写的账号文件夹名称仍可能显示。报告里显示成 [redacted] 的地方，原本是疑似邮箱或账号 ID 的内容，已经遮蔽。",
      "What went wrong? (optional)": "遇到了什么问题？（选填）",
      "Switching to my work account left the personal one closed…": "例如：切换到工作账号后，个人账号没有重新打开……",
      "Copy": "复制",
      "Report a problem": "提交问题反馈",
      "Open a GitHub issue?": "打开 GitHub 问题反馈页？",
      "Copy and open": "复制并打开",
      "The report above and your comment are copied to your clipboard, and your browser opens a new issue on the MCS repository. Paste it there and you can still edit it before submitting.": "上面的报告和你的描述将复制到剪贴板，并在浏览器中打开项目的 GitHub 反馈页。粘贴后仍可编辑，最后由你提交。",
      "GitHub issues are public. What is copied is exactly what you saw on the screen behind this dialog, with your email address, account IDs, user name and home folder already replaced with stand-ins.": "GitHub 问题反馈是公开的。复制的内容就是刚才显示的报告，其中邮箱、账号 ID、用户名和个人文件夹路径已用代号替换。",
      "No date yet": "暂无日期",
      "Unrecognized account": "无法识别的账号",
      "Signed out in Claude Desktop": "已在 Claude 桌面端退出登录",
      "Recover": "恢复",
      "Not signed in yet. Select it here, then switch to it and sign in.": "尚未登录。先在这里勾选，再切换过去登录。",
      "Its conversations are still here. Recover to sign back in.": "会话索引都还在。点「恢复」就可以重新登录。",
      "Sign in to finish setting this up.": "登录之后设置即完成。",
      "Invalid account data": "账号数据无效",
      "Personal": "个人",
      "Free": "免费版",
      "Choose accounts": "选择账号",
      "Which accounts should be managed?": "选择需要管理的账号",
      "Confirm": "确认",
      "It gets its own profile": "创建独立的账号环境",
      "Recover this account": "恢复此账号",
      "different account": "另一个账号",
      "Sign in as a": "请登录",
      ". Signing in as one you already have creates a duplicate, and MCS will ask you to merge.": "。如果登录已添加的账号，就会出现重复环境，切换器会提示合并。",
      "Sign in as the account starting": "请登录账号 ID 以此开头的账号：",
      "Claude closes, your current account is saved, and a clean Claude opens.": "Claude 将关闭并保存当前账号环境，然后打开一个新的登录环境。",
      "Will be archived": "将归档此环境",
      "Keep this one": "保留此环境",
      "In use now · keep this one": "当前使用中 · 保留此环境",
      "Merge duplicates": "合并重复环境",
      "Both are the same account": "这两个环境属于同一个账号",
      "Add at least two managed accounts to sync between them.": "至少添加两个账号，才能进行账号间同步。",
      "Claude closes, then reopens where you were": "Claude 会暂时关闭，完成后回到原账号",
      "Nothing was moved": "未移动任何文件",
      "The account is still on your list, so you can try again.": "账号仍在列表中，可以重试。",
      "Back": "返回",
      "Try again": "重试",
      "Its folder is in your archive, not deleted.": "账号文件夹已归档，文件仍然保留。",
      "It is off the switcher": "已从切换器列表移除",
      "Done": "完成",
      "Switch to another account first, then you can remove it.": "请先切换到另一个账号，再移除此账号。",
      "It comes off your list. Nothing is deleted, and you can sign in to it again any time.": "此账号将从列表移除，文件仍然保留。之后可以随时重新登录。",
      "Switching profile": "正在切换账号",
      "Claude will restart in a moment.": "Claude 即将重新启动。",
      "Switch failed": "切换失败",
      "Switched successfully": "切换成功",
      "Syncing conversations": "正在同步会话索引",
      "Claude closes while this runs, and reopens when it is done.": "操作期间 Claude 会暂时关闭，完成后重新打开。",
      "Sync failed": "同步失败",
      "Sync finished": "同步完成",
      "Sync finished.": "同步完成。",
      "Quit Claude Desktop first, then try Sync again.": "请先退出 Claude 桌面端，再重试同步。",
      "Merging accounts": "正在合并账号环境",
      "Merge failed": "合并失败",
      "Accounts merged": "账号环境已合并",
      "The two accounts are now one.": "同一账号的两个环境已合并为一个。",
      "Adding the account": "正在添加账号",
      "Recovering the account": "正在恢复账号",
      "Account added": "账号已添加",
      "Account recovered": "账号已恢复",
      "Backing up accounts": "正在备份账号",
      "Backup failed": "备份失败",
      "Backup finished": "备份完成",
      "Nothing to back up": "暂无需要备份的内容",
      "No account had any conversations stored yet.": "这些账号尚未保存本地会话索引。",
      "It did not finish.": "操作未能完成。",
      "not signed in": "未登录",
      "Finishing up, then quitting…": "正在完成当前操作，随后退出……",
      "Busy right now. Try the rename again in a moment.": "当前正在执行其他操作，请稍后再修改名称。",
      "That account is no longer recoverable. Run Rescan": "此账号已无法恢复，请重新扫描。",
      "That account is no longer there. Run Rescan": "此账号已不存在，请重新扫描。",
      "that account is no longer there. Run Rescan": "此账号已不存在，请重新扫描。",
      "one of those profiles is no longer there. Run Rescan": "其中一个账号环境已不存在，请重新扫描。",
      "No account was named": "请填写账号名称。",
      "Setting up…": "正在设置……",
      "Removing…": "正在移除……",
      "Gathering debug info…": "正在收集诊断信息……",
      "Copying report…": "正在复制报告……",
      "Report copied. Paste it into the issue.": "报告已复制，请粘贴到 GitHub 反馈页。",
      "Copying…": "正在复制……",
      "Copied.": "已复制。",
      "Checking for updates…": "正在检查更新……",
      "Checking for updates… the answer will pop up in a moment.": "正在检查更新……结果稍后会显示。",
      "An update is already in progress.": "正在更新，请稍候。",
      "Update check failed. Try again later.": "检查更新失败，请稍后重试。",
      "Some conversations were skipped": "部分会话索引已跳过",
      "Recovered": "已恢复"
    }
  };

  const PATTERNS = {
    "zh-Hant": [
      [/^Switch to (.*)\?$/s, m => `要切換到「${m[1]}」嗎？`],
      [/^Claude closes and reopens signed in as (.*)\.$/s, m => `Claude 會關閉，再以「${m[1]}」重新開啟。`],
      [/^Claude is open on (.*)$/s, m => `Claude 目前使用「${m[1]}」`],
      [/^Remove (.*)\?$/s, m => `要移除「${m[1]}」嗎？`],
      [/^Sync into (.*)\?$/s, m => `要同步到「${m[1]}」嗎？`],
      [/^Claude closes, (\d+) conversations? (?:is|are) copied from (.*), then Claude reopens where you were\.$/s, m => `Claude 會關閉，從「${m[2]}」複製 ${m[1]} 則對話，再回到原本的帳號。`],
      [/^It comes off your list\. Its (\d+) conversations? (?:is|are) kept, not deleted\. Signing in to this account again starts a new copy of it, without (?:that conversation|those conversations)\.$/s, m => `這個帳號會從清單移除，${m[1]} 則對話會留在封存資料夾。之後重新登入會開一份新的環境，不會自動帶回這些對話。`],
      [/^(\d+) chats$/, m => `${m[1]} 則對話`],
      [/^\((\d+) chats\)\. Its conversations come back on their own\.$/, m => `（${m[1]} 則對話）。登入之後會自己回來。`],
      [/^(.*) and (.*) are the same account\. Merge them to clean this up\.$/s, m => `「${m[1]}」和「${m[2]}」是同一個帳號，合併之後就只剩一份。`],
      [/^Copy (.*)'s sessions into (.*)$/s, m => `把「${m[1]}」的對話複製到「${m[2]}」`],
      [/^(\d+) accounts? not signed in yet, so it can't be synced\. Switch to it and sign in first\.$/, m => `有 ${m[1]} 個帳號還沒登入，沒辦法同步。請先切換過去登入。`],
      [/^(\d+) conversations exist in both profiles and have changed since they were last in step\. The newer version is kept\. The other stays in the archived folder, which you can open from Settings\.$/, m => `兩份環境裡有 ${m[1]} 則對話都改過，會保留比較新的那一份。另一份留在封存資料夾，可以從設定開啟。`],
      [/^All (\d+) conversations are combined into the account you keep\. The other folder is archived, not deleted, so you can put it back yourself\.$/, m => `全部 ${m[1]} 則對話會合併到你保留的帳號。另一個資料夾會封存起來，不會刪除，你可以自己放回去。`],
      [/^(\d+) files couldn't be read and will be left where they are\.$/, m => `有 ${m[1]} 個檔案讀不到，會留在原處。`],
      [/^Its (\d+) conversations? (?:is|are) in your archive, not deleted\.$/, m => `${m[1]} 則對話已經封存，檔案還在。`],
      [/^(.*) was not removed$/s, m => `沒有移除「${m[1]}」`],
      [/^(.*) removed$/s, m => `已移除「${m[1]}」`],
      [/^You are now on (.*)\.$/s, m => `目前使用「${m[1]}」。`],
      [/^Sign in to (.*) in the Claude window that just opened\.$/s, m => `請在剛開啟的 Claude 視窗裡登入「${m[1]}」。`],
      [/^Backed up (\d+) accounts?\.$/, m => `已經備份 ${m[1]} 個帳號。`],
      [/^(\d+) (?:other )?accounts? could not be backed up \(see the log\)\.$/, m => `有 ${m[1]} 個帳號備份失敗，詳情請看紀錄檔。`],
      [/^Recovered (.*)$/, m => `復原於 ${m[1]}`],
      [/^Could not rename: (.*)$/s, m => `改名稱失敗：${m[1]}`],
      [/^Couldn't copy the report: (.*)$/s, m => `複製報告失敗：${m[1]}`],
      [/^Couldn't copy: (.*)$/s, m => `複製失敗：${m[1]}`],
      [/^(.*) has no account signed in$/s, m => `「${m[1]}」還沒有登入帳號`],
      [/^Copied (\d+) conversations? into (.*)\. (\d+) conversations? (?:was|were) already newer here and left alone\.$/s, m => `已經把 ${m[1]} 則對話複製到「${m[2]}」。那邊有 ${m[3]} 則比較新，保留原樣。`],
      [/^Copied (\d+) conversations? into (.*)\.$/s, m => `已經把 ${m[1]} 則對話複製到「${m[2]}」。`],
      [/^(\d+) files? could not be read and (?:was|were) skipped \(see the log\)\.$/, m => `有 ${m[1]} 個檔案讀不到，已經跳過，詳情請看紀錄檔。`],
      [/^✓ You're on the latest version \(v(.*)\)\.$/s, m => `✓ 已經是最新版本（v${m[1]}）。`],
      [/^Downloading (.*)…$/s, m => `正在下載 ${m[1]}……`],
      [/^Update (.*) is available, but has no download for this platform\.$/s, m => `有新版 ${m[1]}，但是這個平台沒有可以下載的檔案。`],
      [/^Update failed: (.*)$/s, m => `更新失敗：${m[1]}`]
    ],
    "zh-Hans": [
      [/^Switch to (.*)\?$/s, m => `切换到「${m[1]}」？`],
      [/^Claude closes and reopens signed in as (.*)\.$/s, m => `Claude 将关闭，并以「${m[1]}」重新打开。`],
      [/^Claude is open on (.*)$/s, m => `Claude 正在使用「${m[1]}」`],
      [/^Remove (.*)\?$/s, m => `移除「${m[1]}」？`],
      [/^Sync into (.*)\?$/s, m => `同步到「${m[1]}」？`],
      [/^Claude closes, (\d+) conversations? (?:is|are) copied from (.*), then Claude reopens where you were\.$/s, m => `Claude 将关闭，从「${m[2]}」复制 ${m[1]} 条会话索引，再回到原账号。`],
      [/^It comes off your list\. Its (\d+) conversations? (?:is|are) kept, not deleted\. Signing in to this account again starts a new copy of it, without (?:that conversation|those conversations)\.$/s, m => `此账号将从列表移除，${m[1]} 条会话索引会保留在归档中。以后重新登录会创建新环境，不会自动带回这些会话索引。`],
      [/^(\d+) chats$/, m => `${m[1]} 条会话索引`],
      [/^\((\d+) chats\)\. Its conversations come back on their own\.$/, m => `（${m[1]} 条会话索引）。登录后会自动重新显示。`],
      [/^(.*) and (.*) are the same account\. Merge them to clean this up\.$/s, m => `「${m[1]}」和「${m[2]}」属于同一个账号，可合并重复环境。`],
      [/^Copy (.*)'s sessions into (.*)$/s, m => `将「${m[1]}」的会话索引复制到「${m[2]}」`],
      [/^(\d+) accounts? not signed in yet, so it can't be synced\. Switch to it and sign in first\.$/, m => `${m[1]} 个账号尚未登录，无法同步。请先切换并登录。`],
      [/^(\d+) conversations exist in both profiles and have changed since they were last in step\. The newer version is kept\. The other stays in the archived folder, which you can open from Settings\.$/, m => `两个环境中有 ${m[1]} 条会话索引发生冲突，将保留较新的版本。另一版本会留在归档文件夹中，可从设置打开。`],
      [/^All (\d+) conversations are combined into the account you keep\. The other folder is archived, not deleted, so you can put it back yourself\.$/, m => `全部 ${m[1]} 条会话索引将合并到你保留的环境。另一个文件夹会被归档，可手动恢复。`],
      [/^(\d+) files couldn't be read and will be left where they are\.$/, m => `${m[1]} 个文件无法读取，将留在原处。`],
      [/^Its (\d+) conversations? (?:is|are) in your archive, not deleted\.$/, m => `${m[1]} 条会话索引已归档，文件仍然保留。`],
      [/^(.*) was not removed$/s, m => `未能移除「${m[1]}」`],
      [/^(.*) removed$/s, m => `已移除「${m[1]}」`],
      [/^You are now on (.*)\.$/s, m => `当前使用「${m[1]}」。`],
      [/^Sign in to (.*) in the Claude window that just opened\.$/s, m => `请在刚打开的 Claude 窗口中登录「${m[1]}」。`],
      [/^Backed up (\d+) accounts?\.$/, m => `已备份 ${m[1]} 个账号。`],
      [/^(\d+) (?:other )?accounts? could not be backed up \(see the log\)\.$/, m => `${m[1]} 个账号备份失败，详情见日志。`],
      [/^Recovered (.*)$/, m => `恢复于 ${m[1]}`],
      [/^Could not rename: (.*)$/s, m => `修改名称失败：${m[1]}`],
      [/^Couldn't copy the report: (.*)$/s, m => `复制报告失败：${m[1]}`],
      [/^Couldn't copy: (.*)$/s, m => `复制失败：${m[1]}`],
      [/^(.*) has no account signed in$/s, m => `「${m[1]}」尚未登录账号`],
      [/^Copied (\d+) conversations? into (.*)\. (\d+) conversations? (?:was|were) already newer here and left alone\.$/s, m => `已将 ${m[1]} 条会话索引复制到「${m[2]}」。目标中的 ${m[3]} 条会话索引版本较新，已保留。`],
      [/^Copied (\d+) conversations? into (.*)\.$/s, m => `已将 ${m[1]} 条会话索引复制到「${m[2]}」。`],
      [/^(\d+) files? could not be read and (?:was|were) skipped \(see the log\)\.$/, m => `${m[1]} 个文件无法读取，已跳过，详情见日志。`],
      [/^✓ You're on the latest version \(v(.*)\)\.$/s, m => `✓ 已是最新版本（v${m[1]}）。`],
      [/^Downloading (.*)…$/s, m => `正在下载 ${m[1]}……`],
      [/^Update (.*) is available, but has no download for this platform\.$/s, m => `发现更新 ${m[1]}，但暂无此平台的安装包。`],
      [/^Update failed: (.*)$/s, m => `更新失败：${m[1]}`]
    ]
  };

  // Which translation a computer's own language asks for. Traditional covers
  // Taiwan, Hong Kong and Macau; Simplified covers the mainland and Singapore.
  // A bare "zh" stays Simplified, which is what it meant before this file.
  function systemLanguage() {
    const l = (navigator.language || (navigator.languages || [])[0] || 'en')
      .replace(/_/g, '-').toLowerCase();
    if (/^zh-hant(?:-|$)/.test(l) || /^zh-(tw|hk|mo)(?:-|$)/.test(l)) return "zh-Hant";
    if (/^zh-hans(?:-|$)/.test(l) || /^(zh|zh-cn|zh-sg)$/.test(l)) return "zh-Hans";
    return "en";
  }

  const preference = window.__mcsLang || "auto";
  const language = preference === "auto" ? systemLanguage() : preference;
  const words = DICTS[language];
  const patterns = PATTERNS[language];
  if (!words) return;

  function translate(original) {
    const s = original.trim();
    if (!s) return original;
    let result = words[s];
    if (!result) {
      const icon = s.match(/^([＋⟳⚙🏢✓]\s*)(.*)$/s);
      if (icon && words[icon[2]]) result = icon[1] + words[icon[2]];
    }
    if (!result) {
      for (const [regex, format] of patterns) {
        const match = s.match(regex);
        if (match) { result = format(match); break; }
      }
    }
    return result ? original.replace(s, () => result) : original;
  }
  function protectedText(el) {
    if (!el || el.closest('script,style,textarea,code,pre,.dbgbox,.chip')) return true;
    if (el.closest('.name') && el.closest('[data-name],[data-from],.card.selectable')) return true;
    return false;
  }
  function localize(root) {
    if (root.nodeType === Node.TEXT_NODE) {
      if (!protectedText(root.parentElement)) {
        const value = translate(root.nodeValue);
        if (value !== root.nodeValue) root.nodeValue = value;
      }
      return;
    }
    if (root.nodeType !== Node.ELEMENT_NODE && root.nodeType !== Node.DOCUMENT_NODE) return;
    if (root.nodeType === Node.ELEMENT_NODE) {
      for (const attr of ['title','aria-label','placeholder']) {
        if (root.hasAttribute(attr)) {
          const value = translate(root.getAttribute(attr));
          if (value !== root.getAttribute(attr)) root.setAttribute(attr, value);
        }
      }
    }
    if (root.nodeType === Node.ELEMENT_NODE && root.matches('script,style,textarea,code,pre,.dbgbox,.chip')) return;
    for (const child of Array.from(root.childNodes)) localize(child);
  }
  function start() {
    document.documentElement.lang = language;
    document.title = translate(document.title);
    localize(document.body);
    new MutationObserver(records => {
      for (const record of records) {
        if (record.type === 'characterData') localize(record.target);
        else if (record.type === 'childList') for (const node of record.addedNodes) localize(node);
      }
    }).observe(document.body, {subtree:true,childList:true,characterData:true});
    window.mcsI18n = {translate, language};
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once:true});
  else start();
})();
