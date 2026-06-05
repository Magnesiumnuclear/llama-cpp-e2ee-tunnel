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

```bash
curl -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{"message": "你好"}' \
     http://127.0.0.1:8081/api/chat
```

---

### GET /api/conversations — 查看自己的對話記錄（需 L1+）

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8081/api/conversations
```

---

### GET / 及 /api/llama/* — 代理到 llama.cpp Web UI（需 L1+）

> ⚠️ **所有請求都需要認證**，包括 `GET /`。未攜帶 Token 將回傳 401。

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8081/
```

攜帶有效 Token 時，請求被轉發到 `http://127.0.0.1:8080`。
內部 Header（`X-Account-ID`、`X-Device-ID`、`X-Permission`、`Authorization`）不會被轉發給 llama.cpp。

| 路由 | 最低權限 | 說明 |
|------|---------|------|
| `/` | L1 | llama.cpp Web UI 首頁 |
| `/api/llama/*` | L2 | llama.cpp API 端點 |
| `/api/chat` | L2 | 代理層聊天（附審計） |
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
