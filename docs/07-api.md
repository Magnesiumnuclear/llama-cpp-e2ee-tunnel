# HTTP API 端點參考

基礎 URL：`http://127.0.0.1:8081`

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
  "data": { "account_id": "user_001", "status": "pending_approval" }
}
```

錯誤碼：`404` 無效密鑰 | `409` 已使用 | `410` 已過期

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

### POST /admin/approve — 核准帳號

```bash
POST /admin/approve
Content-Type: application/json

{
  "account_id": "user_001",
  "permission_level": "L2"
}
```

回應：
```json
{
  "status": "success",
  "data": { "session_token": "eyJ..." }
}
```

---

### GET /admin/logs — 查看審計日誌

```bash
curl http://127.0.0.1:8081/admin/logs
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
  "account_id": "user_001",
  "device_id": "iPhone-UUID-12345",
  "permission": "L2"
}
```

---

### GET / (及所有 /api/*) — 代理到 llama.cpp

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8081/
```

攜帶有效 Token 時，請求被轉發到 `http://127.0.0.1:8080`。

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
