# 快速啟動指南

## 前置要求

1. **Go 已安裝**：`go version` 能顯示版本
2. **llama.cpp 正在運行**：`http://127.0.0.1:8080` 可訪問
3. Windows PowerShell 或 CMD

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

## 完整測試流程（PowerShell）

```powershell
# 1. 生成 QR Code
$body = @{account_id="test_user"} | ConvertTo-Json
$qr = Invoke-RestMethod -Uri "http://127.0.0.1:8081/admin/generate-qr" -Method POST -ContentType "application/json" -Body $body
$tempKey = $qr.data.temp_key
Write-Host "temp_key: $tempKey"

# 2. 模擬手機掃碼（註冊設備）
$regBody = @{ temp_key=$tempKey; device_id="Test-Device-001" } | ConvertTo-Json
$reg = Invoke-RestMethod -Uri "http://127.0.0.1:8081/auth/register" -Method POST -ContentType "application/json" -Body $regBody
Write-Host "狀態: $($reg.data.status)"

# 3. 電腦端核准
$approveBody = @{ account_id="test_user"; permission_level="L2" } | ConvertTo-Json
$approved = Invoke-RestMethod -Uri "http://127.0.0.1:8081/admin/approve" -Method POST -ContentType "application/json" -Body $approveBody
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
