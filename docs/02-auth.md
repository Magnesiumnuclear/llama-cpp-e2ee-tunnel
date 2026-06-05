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
  解析 QR Code URL → 瀏覽器跳轉到 /auth/register?temp_key=...&account_id=...
  → 伺服器返回認證頁面（HTML）

階段 3：手機發送註冊請求
  頁面自動 POST /auth/register → { temp_key, device_id }
  ← 伺服器回傳 device_secret（一次性，存於頁面記憶體）

階段 4：代理層驗證 temp_key
  查 qr_codes 表：有效 / 未用 / 未過期
  → 標記 used = true
  → 建立 pending_approval 帳號

階段 4.5：手機輪詢核准狀態
  每 2.5 秒 GET /auth/poll?account_id=xxx&device_secret=xxx
  ← 回傳 { status: "pending_approval" }，直到管理員操作

階段 5：電腦端管理員核准
  POST /admin/approve → 設定權限（預設 L2）
  → 生成 session_token（JWT 90天）+ refresh_token（2年）
  → 存入 sessions 資料表

階段 6：手機收到核准通知
  /auth/poll 回傳 { status: "approved" }
  → 伺服器設置 HttpOnly Cookie（session_token，90 天）
  → 瀏覽器自動重導向至 /（Cookie 由瀏覽器自動攜帶）

階段 7：之後的每個請求
  Authorization: Bearer <session_token>  （API 呼叫）
  或瀏覽器自動攜帶 Cookie               （Web UI）

階段 8：代理層驗證
  authMiddleware → validateJWT → 查帳號狀態 → 檢查權限
  → 設置 X-Account-ID / X-Device-ID / X-Permission Header
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

## QR Code 內容格式

QR Code 編碼的是 **URL 格式**，手機掃描後即直接跳轉到註冊頁面：

```
https://your-tunnel.trycloudflare.com/auth/register?temp_key=a1b2c3d4...&account_id=user_001
```

公網 URL 由環境變量 `LLAMA_PUBLIC_URL` 控制（見 [08-quickstart.md](08-quickstart.md)）。
未設定時預設為 `http://127.0.0.1:8081`（僅適合本機測試）。

## 錯誤情境

| 情境 | HTTP 狀態碼 | 原因 |
|------|------------|------|
| temp_key 不存在 | 404 | 無效密鑰 |
| temp_key 已使用 | 409 | 一次性限制 |
| temp_key 已過期 | 410 | 超過 1 小時 |
| device_secret 不符 | 403 | 輪詢時身分驗證失敗 |
| 帳號不存在 | 404 | 輪詢時找不到帳號 |
| JWT 無效 | 401 | 簽名錯誤或過期 |
| 帳號非 active | 403 | 未核准或已停用 |

→ API 端點詳見 [07-api.md](07-api.md)
