# 快速啟動指南

## 前置要求

1. **Go 已安裝**：`go version` 能顯示版本
2. **Node.js 18+ 已安裝**：`node --version`（用於建置 forked Web UI）
3. **llama.cpp 正在運行**：`http://127.0.0.1:8080` 可訪問
4. Windows PowerShell 或 CMD

> **建置 forked Web UI（首次必做）**：proxy 的 `/` 服務的是自架 forked llama-ui，需先建置出 `webui/dist`：
> ```powershell
> cd D:\software\llama\webui
> npm install
> npm run build      # 產出 webui/dist/{index.html,bundle.js,bundle.css}
> ```
> UI 源碼改動後需重新 `npm run build` 並**重啟 proxy**（bundle 於啟動時做 gzip 預壓）。

## 啟動步驟

### Step 1：下載依賴

```powershell
cd D:\software\llama
go mod download
# 或更新
go mod tidy
```

### Step 2：設定環境變量（必須）

```powershell
# 設定 Cloudflare Tunnel 公網 URL（每次開新 terminal 必需設定）
$env:LLAMA_PUBLIC_URL = "https://your-tunnel.trycloudflare.com"

# 沒有 Cloudflare，本機測試用：
# $env:LLAMA_PUBLIC_URL = "http://127.0.0.1:8081"
```

> **為何必填**：QR Code 編碼的是 URL 格式。未設定時手機掃描後的跳轉目的地會是 localhost，外網無法存取。

### Step 3：編譯並運行

```powershell
# 開發模式（直接運行）
go run main.go

# 生產模式（編譯成 .exe）
go build -o llama-proxy.exe main.go
.\llama-proxy.exe
```

啟動成功後應看到：

```
llama.cpp 代理層啟動（階段 3：強制認證版）
✓ 公網 URL: https://your-tunnel.trycloudflare.com
✓ 伺服器密鑰已生成
✓ SQLite 連接成功
✓ 資料庫表格建立完成
🚀 代理層監聽在 :8081
```

## 推薦：用控制面板一鍵啟動

控制面板把上述步驟全自動化：啟動 Cloudflare Tunnel → 自動擷取公網 URL 填入 `LLAMA_PUBLIC_URL` → 帶該 URL 啟動代理層，並提供 QR 生成、權限審核、帳號總覽（含 E2E 測試、刪除）。

> **增量編譯**：啟動代理層時會比對 `main.go` / `go.mod` / `go.sum` 與 `llama-proxy.exe` 的修改時間——**有變動才 `go build`，未變動則直接沿用既有 `llama-proxy.exe`**，省去每次編譯的等待。

介面採 **Qt Quick (QML)**，並做到「前端介面 / 後端邏輯」完全分離：

| 路徑 | 職責 |
|------|------|
| `control_panel.py` | 薄進入點：建立 App → 載入 QML → 注入後端 Controller |
| `backend/proxy_client.py` | 純邏輯：常數、cloudflared 偵測、localhost HTTP（無 GUI 依賴） |
| `backend/models.py` | QML 用清單模型（`DictListModel`） |
| `backend/controller.py` | QObject 橋接層：子程序管理、健康輪詢、業務流程，對 QML 暴露 屬性／訊號／槽 |
| `qml/` | Qt Quick 介面（漸層、動畫、滑動分頁）；`Theme.qml` 為共用設計語彙，其餘為可重用元件與三大分頁 |
| `ui_icons/` | 向量圖示層：以 PyQt 2D 繪圖引擎（QPainter / `VectorIcon`）取代所有 Unicode 符號，純呈現、零後端依賴 |

```powershell
# 首次需安裝（PyQt6 已含 QtQuick / QtQml 模組）：
#   .\.venv\Scripts\python.exe -m pip install -r requirements.txt
.\.venv\Scripts\python.exe control_panel.py
```

> 啟動指令不變；UI 重構不影響任何後端行為與 API。仍需先完成上方「建置 forked Web UI」。

## 完整測試流程（PowerShell）

> **`/admin/*` 在 `127.0.0.1:8082`（僅本機）且需 `X-Admin-Token`**：手動測試時，啟動 proxy 前先設定 `$env:LLAMA_ADMIN_TOKEN = "任意隨機字串"`，admin 請求一律打 **`http://127.0.0.1:8082`** 並帶上 `-Headers @{ "X-Admin-Token" = $env:LLAMA_ADMIN_TOKEN }`（未設定 token 一律 `403`；`/admin/*` 不在對外的 `:8081` 上）。對外端點（`/auth/*`、`/api/*`）仍在 `:8081`。由控制面板啟動時 token 與連接埠都會自動處理。

```powershell
$adminHdr = @{ "X-Admin-Token" = $env:LLAMA_ADMIN_TOKEN }
# 1. 生成 QR Code（admin → :8082）
$body = @{account_id="test_user"} | ConvertTo-Json
$qr = Invoke-RestMethod -Uri "http://127.0.0.1:8082/admin/generate-qr" -Method POST -ContentType "application/json" -Body $body -Headers $adminHdr
$tempKey = $qr.data.temp_key
Write-Host "temp_key: $tempKey"

# 2. 模擬手機掃碼（註冊設備）
$regBody = @{ temp_key=$tempKey; device_id="Test-Device-001" } | ConvertTo-Json
$reg = Invoke-RestMethod -Uri "http://127.0.0.1:8081/auth/register" -Method POST -ContentType "application/json" -Body $regBody
Write-Host "狀態: $($reg.data.status)"

# 3. 電腦端核准
$approveBody = @{ account_id="test_user"; permission="L2"; action="approve" } | ConvertTo-Json
$approved = Invoke-RestMethod -Uri "http://127.0.0.1:8082/admin/approve" -Method POST -ContentType "application/json" -Body $approveBody -Headers $adminHdr
$token = $approved.data.session_token

# 4. 驗證 Token
$headers = @{ Authorization = "Bearer $token" }
Invoke-RestMethod -Uri "http://127.0.0.1:8081/auth/verify" -Headers $headers
```

## 依賴套件

| 套件 | 用途 |
|------|------|
| `github.com/golang-jwt/jwt/v5` | JWT 生成與驗證 |
| `github.com/mattn/go-sqlite3` | SQLite 驅動（需 CGO） |
| `github.com/skip2/go-qrcode` | 生成 QR Code PNG |

## 常見問題

**Q：找不到 JWT 庫**
```powershell
go get github.com/golang-jwt/jwt/v5
```

**Q：`temp_key 已被使用`**
正常！一次性限制。請重新調用 `/admin/generate-qr` 生成新的。

**Q：Token 無效**
檢查 `Authorization: Bearer <token>` 格式是否正確，以及 token 是否在 90 天內。

**Q：SQLite 編譯錯誤（CGO）**
需安裝 GCC，或使用 `go-sqlite3` 的純 Go 替代方案。

**Q：舊資料庫報 `table audit_logs has no column named ip_address`**
不需要删除資料庫。程式啟動時會自動執行 `ALTER TABLE` 補齊缺少的欄位。

**Q：QR Code 掃出來是 JSON 而非 URL**
請確認環境變量 `LLAMA_PUBLIC_URL` 已設定，且對应的是公網可存取的 URL。

→ API 端點完整列表見 [07-api.md](07-api.md)
