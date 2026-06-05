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
| `generateQRHandler()` | 生成 QR Code（管理端） |
| `registerDeviceHandler()` | 設備註冊（手機掃碼後） |
| `approveAccountHandler()` | 電腦端核准帳號 |
| `authMiddleware()` | JWT 驗證中間件 |
| `proxyToLlamaAuthenticated()` | 認證後反向代理到 llama.cpp |

## 技術棧

| 層 | 技術 |
|----|------|
| 語言 | Go |
| 資料庫 | SQLite（WAL 模式） |
| 認證 | JWT HS256 + QR Code 一次性密鑰 |
| 加密 | RSA + AES-256-GCM + HMAC-SHA256（待實作） |
| 網路 | Cloudflare Tunnel（HTTPS） |
| 手機端 | 純網頁 + IndexedDB |

## 當前實作狀態

✅ QR Code 一次性驗證  ✅ JWT（90 天）  ✅ 審計日誌  ✅ SQLite 資料庫
✅ 強制認證中間件（所有路由）  ✅ QR Code URL 格式  ✅ 資料庫自動 Migration
⏳ 端到端加密  ⏳ L1/L2/L3 權限中間件  ⏳ Token 計費  ⏳ 資源限制隊列
