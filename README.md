# Herdr Workspace Enhancer

Go TUI plugin，提供每個 workspace 的文字筆記，以及封存／重新啟用工作環境。支援 Linux 與 Windows。

> 已於 Herdr 0.9.3 驗證 Linux／Windows 原生測試與封存恢復；Windows 下載包可直接 link 啟動 TUI。ARM64 提供交叉編譯產物，尚未在 ARM 硬體實測。

## 安裝與啟動

從 GitHub Releases 下載符合系統／CPU 的壓縮檔：Linux 使用 `.tar.gz`，Windows 使用 `.zip`；一般 Intel／AMD 電腦選 `amd64`，ARM 電腦選 `arm64`。包內包含執行檔與 manifest，**不需要 Go 或自行編譯**。解壓後請保留整個資料夾，Herdr link 會使用此位置。

Linux（進入解壓後資料夾）：

```sh
herdr plugin link "$PWD"
herdr plugin action invoke workspace.enhancer.open
```

Windows PowerShell（進入解壓後資料夾）：

```powershell
herdr plugin link "$PWD"
herdr plugin action invoke workspace.enhancer.open-windows
```

Release 附有每個壓縮檔的 `.sha256` 校驗檔。需要提供 `plugin pane open` 的 Herdr；最低版本為 Herdr 0.9.3。

開發者從原始碼安裝才需要 Go 1.22：先執行 `go build -o bin/ ./cmd/herdr-workspace-enhancer`，再 link repository。

Action 會建立專用的 `Workspace Enhancer` workspace，將 TUI 開在其中。不要直接在專案 workspace 開啟 manager pane。此專用 workspace 不列入可封存清單。

## 操作

| 按鍵 | 行為 |
| --- | --- |
| ↑ / ↓ 或 j / k | 選取 workspace |
| Tab | 全部／啟用中／已封存 |
| / | 搜尋名稱，Enter 結束輸入 |
| e | 編輯筆記；支援中文與多行 Markdown 純文字 |
| Ctrl+S | 儲存筆記 |
| Esc | 返回；未儲存時可選擇儲存、捨棄或繼續編輯 |
| a | 封存確認；y 確認，其他鍵取消 |
| Enter | 切換啟用中的 workspace，或恢復已封存項目 |
| r | 重新整理 |
| q | 離開管理介面 |

封存前先保存 tabs、pane 目錄、名稱與分割版面，再關閉 workspace。重新啟用建立新的 shell，不重跑命令，也不恢復 agent 對話、程序記憶體或終端歷史。獨立 daemon 不會隨之管理。Herdr 拒絕單獨關閉 worktree 群組時，不會自動擴大成整組關閉。

## 儲存與失敗處理

所有資料位於 `HERDR_PLUGIN_STATE_DIR/sessions/<session-hash>/`。目前以 host 注入的 socket／named-pipe 路徑識別 session，不在專案內建立檔案。不要搬動或更改 session endpoint 後假設資料會自動跟隨。

- `workspaces.json`：具版本號的 workspace UUID、Herdr ID、狀態、快照與恢復日誌。
- 筆記使用獨立 JSON 檔，revision 檢查避免兩個編輯器互相覆蓋。
- 作業系統檔案鎖會隨程序結束釋放；寫入先同步暫存檔再替換目標。
- 關閉結果不明時，重新整理會查核 workspace 是否仍存在。
- 恢復時已完成步驟不會重做。不確定步驟會以唯一建立標記或分割前後 pane 清單核對；能確認時接續，歧義或資源缺失時停止並保留日誌。不要自行刪除 pending 日誌後重試。

## 開發驗證

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/herdr-workspace-enhancer
```

CI 在 Ubuntu 與 Windows 執行測試與建置。測試涵蓋筆記衝突、session 隔離、巢狀分割、快照驗證、封存前持久化、恢復日誌、未儲存提示與篩選。這些測試不會關閉你的真實 workspace。

## 自動發布

推送 `v0.1.0` 這類版本 tag 會執行 `.github/workflows/release.yml`：

1. Ubuntu 與 Windows 執行 race tests、vet。
2. 建置 Linux／Windows × amd64／arm64 共四種無 CGO 執行檔。
3. 打包對應平台的 manifest（移除 build hooks）、README、LICENSE 與執行檔。
4. 全部成功後上傳 GitHub Release，附 SHA-256；帶 `-rc.1` 等尾碼的版本標示為 prerelease。

也可在 Actions 手動執行並填入版本，僅產生可下載的 workflow artifacts，不發布 Release。發布流程可重新執行，會更新同名附件。

本機重現打包：`python3 scripts/package.py --os linux --arch amd64 --version v0.1.0`。產物位於 `dist/`。

Linux PTY 冒煙測試：先建置 `bin/herdr-workspace-enhancer`，再執行 `python3 scripts/tui_smoke.py`。此測試使用假的 Herdr 回應，驗證畫面、中文多行筆記輸入、儲存與退出，不操作真實 session。

真實 Herdr 整合測試需自行選定可丟棄的測試 server，設定 `ENHANCER_INTEGRATION=1` 與其 `HERDR_SOCKET_PATH`，再執行 `go test ./internal/workspace -run TestHerdrIntegration -v`。測試只關閉自己建立的 workspaces。

Windows 發布包驗證：`scripts/windows_plugin_smoke.ps1 -HerdrBin <herdr.exe 路徑> -Archive <下載包.zip>`。它使用獨立暫存設定與 session，驗證 link、啟動和畫面後停止測試 server。

發布包可用 `python3 scripts/verify_packages.py dist` 驗證內容、Linux 執行權限與 SHA-256。Workflow 定義已加入 repository；只有推送 tag 後才會在 GitHub 建立 Release。
