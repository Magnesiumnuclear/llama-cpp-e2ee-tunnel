# HTTP API 端點參考

基礎 URL：`http://127.0.0.1:8081`（本機） / `https://your-tunnel.trycloudflare.com`（公網）

## 公開端點（無需認證）

### GET /health — 健康檢查

```bash
curl http://127.0.0.1:8081/health
```

回應：
```json
{ "status": "ok", "message": "代理層正常運行" }
```

---

### GET /api/public-key — 取得伺服器 RSA E2E 公鑰

前端加密前必須先呼叫此端點取得 RSA-2048 SPKI PEM 公鑰，用於 RSA-OAEP 加密每次請求的一次性 `dialogue_key`。

```bash
curl http://127.0.0.1:8081/api/public-key
```

回應：
```json
{
  "status": "success",
  "data": { "public_key": "-----BEGIN PUBLIC KEY-----\nMII...\n-----END PUBLIC KEY-----\n" }
}
```

---

### GET /e2e-test — E2E 加密測試網頁（開發用）

提供互動式 Web Crypto API 測試工具，支援 URL 參數自動填入憑證（由控制面板傳入）。

```
GET /e2e-test
GET /e2e-test?token=eyJ...&secret=a3f9...   ← 控制面板點「E2E 測試」按鈕時帶入
```

> ⚠️ 此頁面僅供開發使用，生產環境建議移除對此路由的服務。

---

### POST /auth/register — 設備註冊（手機掃碼後調用）

```bash
POST /auth/register
Content-Type: application/json

{
  "temp_key": "a1b2c3d4...",
  "device_id": "iPhone-UUID-12345"
}
```

回應（成功）：
```json
{
  "status": "success",
  "message": "設備已註冊，等待電腦端核准",
  "data": {
    "account_id": "user_001",
    "status": "pending_approval",
    "device_secret": "a3f9..."
  }
}
```

> **`device_secret`** 用於輪詢核准狀態（`/auth/poll`），僅此一次回傳，請保存於頁面記憶體中。

錯誤碼：`404` 無效密鑰 | `409` 已使用 | `410` 已過期

---

### GET /auth/poll — 手機端輪詢核准狀態

```bash
GET /auth/poll?account_id=user_001&device_secret=a3f9...
```

回應（待審批）：
```json
{ "status": "success", "data": { "status": "pending_approval" } }
```

回應（已核准）：
```json
{ "status": "success", "data": { "status": "approved" } }
```

> 核准時伺服器會同時設置 **HttpOnly Cookie**（`session_token`），瀏覽器後續請求將自動攜帶。
> 手機頁面收到 `approved` 後自動重導向至 `/`。

回應（rejected / disabled）：
```json
{ "status": "success", "data": { "status": "rejected" } }
```

錯誤碼：`403` device_secret 不符 | `404` 找不到帳號

---

## 管理端點（電腦端調用，無 JWT 保護）

### POST /admin/generate-qr — 生成 QR Code

```bash
POST /admin/generate-qr
Content-Type: application/json

{ "account_id": "user_001" }   // 留空則自動生成 ID
```

回應：
```json
{
  "status": "success",
  "data": {
    "account_id": "user_001",
    "temp_key": "a1b2c3d4...",
    "qr_code_file": "qrcode_user_001.png",
    "expires_at": "2026-06-05 13:00:00"
  }
}
```

---

### GET /admin/pending — 查看待審批帳號

```bash
curl http://127.0.0.1:8081/admin/pending
```

---

### GET /admin/accounts — 查看所有帳號

```bash
curl http://127.0.0.1:8081/admin/accounts
```

回應欄位：`account_id`、`device_id`、`permission`、`status`、`created_at`、`approved_at`、`last_login`

---

### POST /admin/approve — 核准帳號

```bash
POST /admin/approve
Content-Type: application/json

{
  "account_id": "user_001",
  "permission": "L2",      // L1 / L2 / L3，預設 L2
  "action": "approve"     // 或 "reject"，預設 approve
}
```

回應：
```json
{
  "status": "success",
  "data": {
    "account_id": "user_001",
    "permission": "L2",
    "session_token": "eyJ...",
    "refresh_token": "eyJ...",
    "expires_at": "2026-09-03 14:00:00"
  }
}
```

---

### GET /admin/logs — 查看審計日誌

```bash
# 預設最新 50 筆；?limit=N 可調整（上限 1000）
curl http://127.0.0.1:8081/admin/logs
curl http://127.0.0.1:8081/admin/logs?limit=100
```

---

### GET /admin/account-secrets — 取得帳號憑證（供控制面板 E2E 測試用）

回傳指定 `active` 帳號的最新 `session_token` 與 `device_secret`，供控制面板一鍵開啟 E2E 測試頁使用。

```bash
curl "http://127.0.0.1:8081/admin/account-secrets?account_id=user_001"
```

回應（成功）：
```json
{
  "status": "success",
  "data": {
    "account_id": "user_001",
    "session_token": "eyJ...",
    "device_secret": "a3f9b2c1..."
  }
}
```

錯誤碼：`400` 缺少 account_id | `403` 帳號非 active | `404` 帳號不存在 | `500` 找不到 Session

> ⚠️ 此端點回傳高敏感資料，生產環境**必須**額外加上 IP 白名單（僅允許 127.0.0.1 存取）。

---

## 需認證的端點（Bearer Token）

### GET /auth/verify — 驗證 Token

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8081/auth/verify
```

回應：
```json
{
  "status": "valid",
  "data": {
    "account_id": "user_001",
    "device_id": "iPhone-UUID-12345",
    "permission": "L2",
    "expires_at": "2026-09-03 14:00:00"
  }
}
```

---

### POST /api/chat — 發送聊天訊息（需 L2+）

同時支援**明文**與 **E2E 加密**兩種格式，根據請求 body 是否含 `encrypted_key` 欄位自動切換。

**明文格式（向後相容）：**
```bash
curl -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{"message": "你好"}' \
     http://127.0.0.1:8081/api/chat
```

**E2E 加密格式（推薦）：**
```json
{
  "encrypted_key":  "<base64: RSA-OAEP 加密的 AES-256 dialogue_key>",
  "ciphertext":     "<base64: AES-256-GCM 密文，末尾含 16-byte GCM tag>",
  "nonce":          "<base64: 12-byte AES-GCM nonce>",
  "hmac_signature": "<base64: HMAC-SHA256，簽名對象：b64(encrypted_key).b64(nonce).b64(ciphertext)>"
}
```

→ E2E 加密細節見 [04-encryption.md](04-encryption.md)

---

### POST /api/e2e/chat — forked UI 的聊天串流端點（需 L2+）

自架 forked llama-ui 的 `ChatService` 改打此端點（取代直接打 llama.cpp 原生的 `/v1/chat/completions`，見 [01-architecture.md](01-architecture.md)）。

流程：proxy 讀請求 body → 轉發 llama.cpp `/v1/chat/completions`（串流）→ 逐塊 flush 回前端。

- **P2（目前）**：明文串流轉發，驗證自訂串流管線。
- **P3（規劃）**：proxy 在此「解密入、逐塊 AES-GCM 加密出」，前端對應加密送/解密收，使聊天內容對 Cloudflare 不可見。

回應：`text/event-stream`（SSE，與 OpenAI `/v1/chat/completions` 格式相容，前端 SSE 解析不需改動）。

---

### GET /api/conversations — 查看自己的對話記錄（需 L1+）

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8081/api/conversations
```

---

### GET / 及前端資源、其餘路徑 — Web UI 與 llama.cpp 代理（需 L1+）

> ⚠️ **所有請求都需要認證**，包括 `GET /`。未攜帶 Token 將回傳 401。

`/`、`/bundle.js`、`/bundle.css` 由 proxy **服務本地自架 forked llama-ui**（`webui/dist`，bundle 經 gzip 預壓），**不再轉發** llama.cpp 的原生 UI。其餘路徑（`/props`、`/v1/*`、`/slots`、`/models`、`/tools` …）才轉發到 `http://127.0.0.1:8080`。
內部 Header（`X-Account-ID`、`X-Device-ID`、`X-Permission`、`Authorization`）不會被轉發給 llama.cpp。

| 路由 | 最低權限 | 說明 |
|------|---------|------|
| `/`、`/bundle.js`、`/bundle.css` | L1 | 服務 forked llama-ui（本地 `webui/dist`） |
| `/api/e2e/chat` | L2 | forked UI 的聊天串流端點（轉發 llama.cpp） |
| `/props`、`/v1/*`、`/slots` … | L1 | 轉發 llama.cpp（UI 的 metadata 與原生 API） |
| `/api/llama/*` | L2 | llama.cpp API 端點 |
| `/api/chat` | L2 | 代理層聊天（明文/E2E 解密；目前回模擬回應） |
| `/api/conversations` | L1 | 對話歷史查詢 |
| `/auth/verify` | L1 | Token 狀態確認 |

---

## 通用回應格式

```json
{
  "status": "success|error",
  "message": "說明文字（可選）",
  "data": {},
  "error": "錯誤說明（僅 error 時出現）"
}
```

→ 認證流程細節見 [02-auth.md](02-auth.md)
