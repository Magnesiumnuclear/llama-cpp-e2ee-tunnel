# 端到端加密（E2E）與密鑰管理

> **目前狀態**：⏳ 待實作（架構設計完成）

## 加密算法組合

| 用途 | 算法 |
|------|------|
| 訊息加密 | AES-256-GCM |
| 密鑰傳輸 | RSA（手機公鑰加密 dialogue_key） |
| 請求完整性 | HMAC-SHA256（device_secret 簽名） |
| 私鑰保護 | PBKDF2（密碼 → 衍生密鑰） + AES-256-GCM |

## 手機端加密流程（發送）

```
明文訊息
  → 生成本次 dialogue_key（隨機 AES 密鑰）
  → AES-256-GCM 加密訊息（ciphertext + nonce + tag）
  → HMAC-SHA256 簽名（用 device_secret）
  → RSA 加密 dialogue_key（用伺服器 E2E 公鑰）
  → 組裝請求 → HTTPS POST /chat
```

## 電腦端解密流程（接收）

```
驗證 JWT session_token
  → 查 device_secret → 驗證 HMAC 簽名
  → RSA 解密 dialogue_key（用伺服器私鑰）
  → AES 解密訊息 → 得明文
  → 權限檢查 → 審計日誌 → 轉發 llama.cpp（本機明文）
```

## Cloudflare 可見性

| 欄位 | Cloudflare 能看到？ |
|------|:---:|
| session_token | 可見（但無法驗證，簽名密鑰在電腦端） |
| ciphertext（訊息密文） | ✗ 完全密文 |
| encrypted_key（RSA 加密的 dialogue_key） | ✗ 完全密文 |
| nonce / timestamp | 可見（無意義） |

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
