# 系統架構

## 整體架構圖

```
┌─────────────┐     端到端加密      ┌──────────────┐
│  手機瀏覽器  │ ─────────────────→ │  Cloudflare  │
│ (純 Web)    │ ←───────────────── │    Tunnel    │
│             │      HTTPS         │ (看不到明文)  │
│ IndexedDB   │                    └──────┬───────┘
│ + 密碼保護  │                           │
└─────────────┘                           │ HTTPS
                                          ▼
                            ┌─────────────────────────┐
                            │     運行 llama.cpp 的電腦  │
                            │                         │
                            │  ┌───────────────────┐  │
                            │  │  代理層 (Port 8081) │  │
                            │  │  - 認證 (JWT)      │  │
                            │  │  - 權限檢查        │  │
                            │  │  - E2E 解密        │  │
                            │  │  - Token 計費      │  │
                            │  │  - 審計日誌        │  │
                            │  └─────────┬─────────┘  │
                            │            │ 本機轉發     │
                            │            ▼            │
                            │  ┌───────────────────┐  │
                            │  │  llama.cpp server  │  │
                            │  │  Port 8080        │  │
                            │  └───────────────────┘  │
                            │            │            │
                            │            ▼            │
                            │      ┌──────────┐       │
                            │      │  SQLite  │       │
                            │      └──────────┘       │
                            └─────────────────────────┘
```

## 專案定位

以 llama.cpp 為實驗載體，建立多用戶權限隔離 + 端到端加密的存取系統，作為日後 **EyeSeeMore** 圖片管理專案的參考範例。

## 關鍵設計決策

| 項目 | 選擇 | 理由 |
|------|------|------|
| 網路模式 | 公網暴露（Cloudflare Tunnel） | 任何地點都能用 |
| 加密層級 | 端到端加密（E2E） | Cloudflare 也看不到明文 |
| 代理層位置 | 獨立服務（Port 8081） | 模塊化，與 llama.cpp 解耦 |
| 手機端 | 純網頁 + IndexedDB | 不需上架 App Store |
| 私鑰保護 | IndexedDB + 密碼（PBKDF2 + AES-256-GCM） | 即使資料外洩也無法解密 |
| 傳輸協議 | 全程 HTTPS | 雙重加密保險 |
| 伺服器存儲 | SQLite | 輕量、足夠專業 |
| Session 策略 | 自動刷新（Refresh Token） | 用戶無感知 |
| 多設備 | 多個獨立 Session | 各設備獨立額度 |
| 資源限制 | 優先級隊列 | 公平調度 |

## forked Web UI 與 E2E 聊天中繼

為了讓「聊天內容對 Cloudflare 也不可見」，原生 llama.cpp Web UI（第三方 bundle、串流回應）無法直接加密，因此改採 **fork 其 SvelteKit 源碼**（`webui/`，由 llama.cpp 的 `tools/ui` 複製而來），僅於網路層 `ChatService` 注入加解密、**不改聊天渲染與推理邏輯**。proxy 同時擔任「UI 主機」與「E2E 聊天閘道」，llama.cpp 退居純後端：

```
手機/瀏覽器 ──HTTPS隧道──→ 代理層 :8081
  GET / /bundle.js …       → 本地服務 forked llama-ui（webui/dist，gzip 預壓）
  POST /api/e2e/chat       → e2eChatHandler：串流中繼
  GET /props /v1/models …  → 轉發 llama.cpp :8080
```

| 階段 | 內容 | 狀態 |
|------|------|------|
| P1 | proxy 服務 forked UI、其餘轉發 llama.cpp | ✅ |
| P2 | 聊天改走 `/api/e2e/chat` 自訂串流（明文） | ✅ |
| P3 | `/api/e2e/chat` 解密入/逐塊 AES-GCM 加密出 + 前端加解密 | ✅ |
| P4 | 文檔同步、gzip 等優化 | ✅ |

> 範圍：僅加密**聊天內容**（prompt + 回應）；metadata（`/props`、`/v1/models` …）維持明文。
> 注意：舊端點 `/api/chat` 目前 E2E 解密成功但回傳模擬回應；實際聊天走 forked UI 的 `/api/e2e/chat`。

## 六張核心資料表

```
qr_codes      → 一次性密鑰（掃碼註冊 + 重新登入共用，以 kind 區分）
accounts      → 帳號與設備資訊
sessions      → JWT session/refresh token
conversations → 對話記錄（含 token 統計）
token_usage   → 每日 token 用量
audit_logs    → 永久審計日誌
```

→ 完整 Schema 見 [06-database.md](06-database.md)
