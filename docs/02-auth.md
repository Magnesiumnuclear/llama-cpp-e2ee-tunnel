# 認證流程（QR Code + JWT）

## 核心規則

- 電腦生成 QR Code，內含一次性 `temp_key`（1 小時有效期）
- **一個 QR Code 對應一個帳號**
- **掃描後立即失效**（`used = true`），需重新生成
- 成功連接後由**電腦端核准並設定權限**（預設 L2）
- Session Token：JWT HS256，有效期 **90 天**

## 八階段完整流程

```
階段 1：電腦生成 QR Code
  POST /admin/generate-qr → 生成 temp_key → 存入 qr_codes 表 → 輸出 QR 圖片

階段 2：手機掃描
  解析 QR Code → 取得 temp_key + account_id

階段 3：手機發送註冊請求
  POST /auth/register → { temp_key, device_id }

階段 4：電腦驗證 temp_key
  查 qr_codes 表：有效 / 未用 / 未過期
  → 標記 used = true
  → 建立 pending_approval 帳號

階段 5：電腦端管理員核准
  POST /admin/approve → 設定權限（預設 L2）
  → 生成 session_token（JWT 90天）+ refresh_token（2年）
  → 回傳給手機

階段 6：手機接收並存儲
  將 session_token 存入 IndexedDB（後續加密保護）

階段 7：之後的每個請求
  Authorization: Bearer <session_token>

階段 8：代理層驗證
  authMiddleware → validateJWT → 設置 X-Account-ID / X-Permission Header
```

## 帳號狀態機

```
[不存在] →(首次掃描)→ [pending_approval] →(核准)→ [active]
                              ↓(拒絕)                  ↓(撤銷)
                          [rejected]               [disabled]
```

## JWT Payload

```json
{
  "account_id": "user_001",
  "device_id": "iPhone-UUID-12345",
  "permission": "L2",
  "exp": 1779849305,
  "iat": 1748313305,
  "jti": "jwt_a1b2c3d4"
}
```

## QR Code 包含的欄位

```json
{
  "temp_key": "a1b2c3d4e5f6...",
  "account_id": "user_001",
  "expires_at": "2026-06-05T12:00:00Z"
}
```

## 錯誤情境

| 情境 | HTTP 狀態碼 | 原因 |
|------|------------|------|
| temp_key 不存在 | 404 | 無效密鑰 |
| temp_key 已使用 | 409 | 一次性限制 |
| temp_key 已過期 | 410 | 超過 1 小時 |
| JWT 無效 | 401 | 簽名錯誤或過期 |

→ API 端點詳見 [07-api.md](07-api.md)
