# 端到端加密（E2E）與密鑰管理

> **目前狀態**：✅ 已實作。實際 E2E 聊天走 forked UI 的 `/api/e2e/chat`（請求/回應雙向加密、真實串流推論）；`/api/chat` 為單次 E2E 解密示範（解密成功但回模擬回應）。

## 加密算法組合

| 用途 | 算法 |
|------|------|
| 訊息加密 | AES-256-GCM |
| 密鑰傳輸 | RSA-2048-OAEP（SHA-256）（手機用伺服器公鑰加密 dialogue_key） |
| 請求完整性 | HMAC-SHA256（device_secret 簽名） |
| 私鑰保護 | PBKDF2（密碼 → 衍生密鑰）+ AES-256-GCM（IndexedDB 內儲存） |

## 伺服器密鑰對（main.go 實作）

| 檔案 | 內容 | 權限 |
|------|------|------|
| `server_private.pem` | RSA-2048 PKCS#8 私鑰 | 0600（僅擁有者可讀） |
| `server_public.pem` | RSA-2048 SPKI 公鑰 | 0644 |

啟動時 `loadOrGenerateRSAKeyPair()` 自動處理：
- 若 `server_private.pem` 已存在：載入並驗證密鑰
- 若不存在或解析失敗：自動生成新密鑰對並寫入磁碟

## 手機端加密流程（發送）

```
明文訊息
  → 生成本次 dialogue_key（隨機 AES-256-GCM key，crypto.getRandomValues）
  → 生成隨機 12-byte nonce
  → AES-256-GCM 加密訊息（ciphertext 末尾含 16-byte GCM tag）
  → RSA-2048-OAEP（SHA-256）加密 dialogue_key（用伺服器 E2E 公鑰）
  → HMAC-SHA256 簽名（簽名對象：b64(encrypted_key).b64(nonce).b64(ciphertext)）
  → 組裝請求 → HTTPS POST /api/chat
```

## 電腦端解密流程（接收）

```
驗證 JWT session_token（authMiddleware）
  → 檢查帳號狀態為 active
  → 查 device_secret → HMAC-SHA256 驗證（constant-time 比較防 timing attack）
  → RSA-2048-OAEP 解密 encrypted_key，取得 dialogue_key（32 bytes）
  → AES-256-GCM 解密 ciphertext（GCM tag 驗證密文完整性）
  → 得明文 JSON → 權限檢查 → 審計日誌 → 轉發 llama.cpp（本機明文）
```

## 實作相關檔案

| 檔案 | 說明 |
|------|------|
| `main.go`（/api/chat 單次 E2E） | `loadOrGenerateRSAKeyPair()`、`decryptE2ERequest()`、`publicKeyHandler()` |
| `main.go`（/api/e2e/chat 串流 E2E） | `e2eChatHandler()`、`decryptE2EChat()`（RSA+AES，回傳明文與金鑰 K）、`encE2EFrame()`（逐塊 AES-GCM 加密成 SSE 幀） |
| `webui/src/lib/services/e2e-crypto.ts` | forked UI 前端：取公鑰、產 K、加密請求、解密回應 SSE 幀串流（匯出 `e2eFetch`） |
| `webui/src/lib/services/chat.service.ts` | 兩個 fetch 點改用 `e2eFetch`（聊天請求加密送、回應解密收） |
| `e2e_test.html` | /api/chat 的前端測試頁（Web Crypto API，模擬手機端加密發送） |
| `server_private.pem` | 伺服器 RSA 私鑰（自動生成，不入 git） |
| `server_public.pem` | 伺服器 RSA 公鑰（自動生成） |

## HTTP API

| 端點 | 說明 |
|------|------|
| `GET /api/public-key` | 返回伺服器 RSA E2E 公鑰（SPKI PEM），公開無需認證 |
| `POST /api/chat` | 同時支援明文請求與 E2E 加密請求（附 HMAC），需 L2+ |
| `POST /api/e2e/chat` | forked UI 的 E2E 聊天串流（無 HMAC，AES-GCM 加密回應），需 L2+ |
| `GET /e2e-test` | E2E 測試頁（開發用） |

### E2E Payload 格式

```json
{
  "encrypted_key":  "<base64: RSA-OAEP 加密的 AES-256 dialogue_key>",
  "ciphertext":     "<base64: AES-256-GCM 密文，末尾含 16-byte GCM tag>",
  "nonce":          "<base64: 12-byte AES-GCM nonce>",
  "hmac_signature": "<base64: HMAC-SHA256 簽名>"
}
```

**HMAC 簽名對象**（必須與前端一致）：
```
base64(encrypted_key) + "." + base64(nonce) + "." + base64(ciphertext)
```

| 欄位 | Cloudflare 能看到？ |
|------|:---:|
| session_token | 可見（但無法驗證，簽名密鑰在電腦端） |
| ciphertext（訊息密文） | ✗ 完全密文 |
| encrypted_key（RSA 加密的 dialogue_key） | ✗ 完全密文 |
| nonce / timestamp | 可見（無意義） |

## `/api/e2e/chat` 信封格式（forked UI 使用）

與 `/api/chat` 的 E2E 格式不同，此端點使用更精簡的信封（認證靠 HttpOnly cookie，不需 HMAC）：

### 請求信封（`e2eChatEnvelope`）

```json
{
  "encrypted_key": "<base64: RSA-OAEP(伺服器公鑰, K)>",
  "iv":            "<base64: 12-byte AES-GCM nonce>",
  "ciphertext":    "<base64: AES-256-GCM(K, iv, OpenAI 請求 JSON)，末尾含 GCM tag>"
}
```

| 欄位對比 | `/api/chat` | `/api/e2e/chat` |
|---------|:-----------:|:---------------:|
| nonce 欄位名 | `nonce` | `iv` |
| HMAC 驗證 | ✓ `hmac_signature` | ✗（靠 AES-GCM tag） |
| 認證方式 | Bearer Token | HttpOnly Cookie |

### 回應格式（加密 SSE 串流）

Proxy 用同一把 K 對 llama.cpp 串流回應逐塊加密，每個 SSE 幀格式：
```
data: <base64(iv)>.<base64(ciphertext+tag)>\n\n
```
前端解密：從 `data:` 取出字串 → 以 `.` 分割 → base64 解碼 iv 與 ct → AES-GCM 解密 → 得原始 OpenAI SSE 片段。

> **金鑰 K**：每次請求由前端新生一把（RSA-OAEP 傳輸給伺服器），proxy 解密後以同一把 K 加密該次回應，請求結束即丟棄 —— 無伺服器端金鑰快取、不依賴 device_secret。
> **加密範圍**：僅聊天內容（prompt + 回應）。metadata 端點（`/props`、`/v1/models`、`/slots` …）仍明文轉發。
> **下游注意**：以下「IndexedDB 密鑰存儲」「手機重啟重連」等節描述的是 `/api/chat` 原始手機端設計；forked UI 的 `/api/e2e/chat` 採每請求金鑰 + Cookie 認證，不使用這些。

## IndexedDB 密鑰存儲結構（手機端）

```
objectStore: 'keys'
├─ private_key_main
│   ├─ encrypted      (AES-256-GCM 密文)
│   ├─ salt           (隨機，防字典攻擊)
│   ├─ algorithm      'AES-256-GCM'
│   ├─ device_id
│   └─ created_at
├─ session_token { value, expires_at }
├─ device_secret { value, never_expires: true }
└─ cache_conversations { ..., last_sync }
```

## 密鑰生命週期

| 密鑰 | 生成位置 | 存儲 | 加密方式 | 失效時機 |
|------|--------|------|--------|--------|
| temp_key | 電腦 | SQLite | 明文 | 使用後立即 |
| device_private_key | 手機 | IndexedDB | 密碼 + AES | 帳號刪除 |
| device_public_key | 手機 | SQLite | 明文（公鑰） | 帳號刪除 |
| session_token | 電腦 | IndexedDB | JWT 簽名 | 90 天 |
| refresh_token | 電腦 | IndexedDB | JWT 簽名 | 2 年 |
| dialogue_key | 手機（每次） | 記憶體 | — | 對話結束清空 |

## 手機重啟後的重連流程

```
手機重啟 → 開網頁 → 檢查 IndexedDB
  ├─ 有 private_key → 提示輸入密碼 → 解密 → 自動登入
  │     └─ session 過期？→ 用 refresh_token 自動換新
  └─ 無 private_key → 提示重新掃 QR Code
```

## 威脅模型

| 攻擊情境 | 結果 |
|------|------|
| Cloudflare 被入侵 | ✗ 無法解密（密鑰不在中間） |
| 電腦磁盤被複製 | ⚠️ 可讀 device_secret，需定期審查設備 |
| IndexedDB 被導出 | ✗ 無密碼無法解密私鑰 |
| XSS 攻擊 | ⚠️ 記憶體中明文私鑰有風險 → 需 CSP + SRI + 密碼門檻 |
