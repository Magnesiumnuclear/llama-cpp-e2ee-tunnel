# llama-proxy 專案導覽

> **AI 請優先閱讀此檔案**，再依需求閱讀下方對應的詳細文件。

**定位**：llama.cpp 多用戶加密存取系統的 Go 代理層（Port 8081）。坐落於 llama.cpp（Port 8080）與手機瀏覽器之間，負責認證、權限、E2E 加密、Token 計費與審計日誌。

## 系統流程

```
手機瀏覽器 → Cloudflare Tunnel → 代理層 :8081 → llama.cpp :8080
                                      ↕
                                   SQLite
```

## 文件索引

| 主題 | 檔案 |
|------|------|
| 系統架構圖與設計決策 | [01-architecture.md](01-architecture.md) |
| QR Code 認證與 JWT 流程 | [02-auth.md](02-auth.md) |
| L1/L2/L3 三級權限系統 | [03-permissions.md](03-permissions.md) |
| 端到端加密與密鑰管理 | [04-encryption.md](04-encryption.md) |
| Token 計費與資源限制 | [05-token-billing.md](05-token-billing.md) |
| SQLite 資料庫 Schema | [06-database.md](06-database.md) |
| HTTP API 端點參考 | [07-api.md](07-api.md) |
| 環境建置與快速啟動 | [08-quickstart.md](08-quickstart.md) |

## 程式碼入口（main.go）

| 函式 | 職責 |
|------|------|
| `initDB()` | 建立 SQLite 資料表 |
| `loadOrGenerateRSAKeyPair()` | 啟動時自動載入或生成 RSA-2048 E2E 密鑰對 |
| `decryptE2ERequest()` | E2E 解密：HMAC 驗證 → RSA-OAEP 解密 → AES-256-GCM 解密 |
| `generateQRHandler()` | 生成 QR Code（管理端） |
| `registerDeviceHandler()` | 設備註冊（GET 返回 HTML 頁面；POST 驗證 temp_key） |
| `pollStatusHandler()` | 手機輪詢核准狀態，核准時種 HttpOnly Cookie |
| `approveAccountHandler()` | 電腦端核准 / 拒絕帳號 |
| `listAccountsHandler()` | 列出所有帳號 |
| `accountSecretsHandler()` | 取得帳號 session_token + device_secret（供控制面板 E2E 測試用） |
| `reloginCodeHandler()` | 為既有 active 帳號鑄造一次性重新登入 code + QR（管理端） |
| `reloginHandler()` | 公開重新登入：GET 顯示 CSRF 確認頁、POST 消耗 code 並重簽 JWT 種 Cookie |
| `adminGate()` | 包住所有 /admin/* 端點，要求 X-Admin-Token（防公網 tunnel 直呼） |
| `viewLogsHandler()` | 查看審計日誌（支援 ?limit=N） |
| `publicKeyHandler()` | 回傳伺服器 RSA E2E 公鑰（SPKI PEM） |
| `chatHandler()` | 聊天端點（L2+，支援明文與 E2E 加密，附審計日誌） |
| `myConversationsHandler()` | 查詢自己的對話記錄（L1+） |
| `authMiddleware()` | JWT 驗證 + 帳號狀態 + 權限等級中間件 |
| `checkPermission()` | L1/L2/L3 等級比較（userLevel >= requiredLevel） |
| `uiOrProxyHandler()` | 服務 forked llama-ui（`webui/dist`，gzip）；其餘路徑轉發 llama.cpp |
| `e2eChatHandler()` | forked UI 的聊天串流端點 `/api/e2e/chat`（轉發 llama.cpp `/v1/chat/completions`） |
| `proxyToLlamaAuthenticated()` | 認證後反向代理到 llama.cpp |

## 技術棧

| 層 | 技術 |
|----|------|
| 語言 | Go |
| 資料庫 | SQLite（WAL 模式） |
| 認證 | JWT HS256 + QR Code 一次性密鑰 |
| 加密 | RSA-2048-OAEP + AES-256-GCM + HMAC-SHA256 |
| 網路 | Cloudflare Tunnel（HTTPS） |
| 手機端 | 純網頁 + IndexedDB |

## 當前實作狀態

✅ QR Code 一次性驗證  ✅ JWT（90 天）  ✅ 審計日誌  ✅ SQLite 資料庫
✅ 強制認證中間件（所有路由）  ✅ QR Code URL 格式  ✅ 資料庫自動 Migration
✅ L1/L2/L3 權限中間件  ✅ 手機端輪詢核准（HttpOnly Cookie）  ✅ RSA E2E 密鑰對自動生成
✅ 端到端加密（AES-256-GCM + RSA-OAEP + HMAC）  ✅ E2E 測試頁 /e2e-test
✅ /api/e2e/chat 濫用防禦（iv 防重放 + 每帳號速率限制 + 串流併發上限）
✅ 自架 forked llama-ui（proxy 服務 + gzip 預壓）  ✅ 聊天串流 E2E 加密 /api/e2e/chat（P3）
✅ 重新登入（一次性 code + CSRF 確認頁，換網址/重啟/關頁後恢復）  ✅ /admin/* X-Admin-Token 防護
⏳ Token 計費  ⏳ 資源限制隊列
