# llama.cpp 多用戶加密存取系統 — 架構規格書

> **專案定位**：以 llama.cpp 為實驗載體，建立一套多用戶權限隔離 + 端到端加密的存取系統，作為日後 **EyeSeeMore** 圖片管理專案的參考範例。
>
> **版本**：1.0 ｜ **狀態**：架構設計完成，待實作

---

## 1. 專案目標

| 目標 | 說明 |
|------|------|
| 多用戶隔離 | User 1/2/3 各自獨立對話，互相看不到彼此資料 |
| 權限分級 | L1/L2/L3 三級權限，沿用 EyeSeeMore 的讀/寫/刪模型 |
| 端到端加密 | 即使走公網（Cloudflare），中間人也看不到明文 |
| 純網頁存取 | 手機端僅用瀏覽器，不開發原生 App |
| Token 計費 | 實時追蹤每個用戶的 token 消耗 |
| 完整審計 | 永久保存所有操作日誌，僅電腦端可查 |

---

## 2. 系統架構總覽

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
                            │  │  代理層 (獨立服務)  │  │
                            │  │  Port 8081        │  │
                            │  │  - 認證           │  │
                            │  │  - 權限檢查        │  │
                            │  │  - E2E 解密       │  │
                            │  │  - Token 計費     │  │
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

### 關鍵設計決策

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
| 對話同步 | 自動同步（實時） | 多設備一致 |
| 多設備 | 多個獨立 Session | 各設備獨立額度 |
| 資源限制 | 優先級隊列 | 公平調度 |

---

## 3. 權限系統（沿用 EyeSeeMore 模型）

### 3.1 三級權限定義

| 權限 | 名稱 | 對話讀 | 對話寫 | 對話改 | 對話刪 | EyeSeeMore 對應 |
|------|------|:---:|:---:|:---:|:---:|------|
| **L1** | 唯讀 | ✓ | ✗ | ✗ | ✗ | 只讀資料庫圖片 + 下載 |
| **L2** | 讀寫改刪（預設） | ✓ | ✓ | ✓ | ✓ | 正常權限 |
| **L3** | 完整存取 | ✓ | ✓ | ✓ | ✓ | 目前同 L2，ESM 才加額外功能 |

> **預設權限**：新帳號未經設定時為 **L2**。

### 3.2 權限邊界

```
【自己的對話】
讀取對話歷史         L1 ✓  L2 ✓  L3 ✓
讀取對話詳情         L1 ✓  L2 ✓  L3 ✓
匯出對話 (CSV/PDF)   L1 ✓  L2 ✓  L3 ✓
新建對話 / 發送 prompt   L1 ✗  L2 ✓  L3 ✓
編輯自己的 prompt    L1 ✗  L2 ✓  L3 ✓
刪除自己的對話       L1 ✗  L2 ✓  L3 ✓

【別人的對話】
任何操作             一律 ✗（隱私保護，永不允許）

【系統資料】（僅電腦端可查）
Token 統計 / 系統日誌 / 用戶列表 / 權限配置   一律 ✗
```

### 3.3 L1 無資料時的回應

```json
{
  "status": "success",
  "data": [],
  "message": "無資料"
}
```

---

## 4. 認證流程（QR Code 一次性密鑰）

### 4.1 核心規則

- 電腦生成 QR Code，內含一次性 `temp_key`
- **一個 QR Code 對應一個帳號**
- **掃描後立即失效**（`used = true`），需重新生成才有新位置
- QR Code 有效期 1 小時
- 成功連接後由**電腦端核准並設定權限**（預設 L2）

### 4.2 完整流程（八階段）

```
階段 1：電腦生成 QR Code
  生成 temp_key + device_secret + device_id
  組裝下載連結 + HMAC 簽名 → 編碼成 QR Code 顯示

階段 2：手機掃描
  解析 QR Code → 驗證簽名 + 過期時間 → 立即清除 QR 圖像

階段 3：手機生成本地密鑰對
  產生 RSA 私鑰（留在手機）+ 公鑰（送伺服器）
  用 E2E 公鑰加密註冊資料 → POST /auth/register_device

階段 4：電腦驗證
  驗證 temp_key（有效 / 未用 / 未過期 / 簽名正確）
  標記 temp_key 為已使用 → 建立 pending_approval 帳號

階段 5：電腦用戶核准
  設定權限（預設 L2）→ 生成 session_token / device_secret / refresh_token
  用手機公鑰加密令牌包 → 回傳

階段 6：手機接收 + 存儲
  用本地私鑰解密令牌包
  設定密碼 → PBKDF2 + AES-256-GCM 加密私鑰 → 存入 IndexedDB
  清空臨時資料

階段 7：發送加密請求（見第 6 章）

階段 8：記錄與計費（見第 7、9 章）
```

### 4.3 帳號狀態機

```
[不存在] →(首次掃描)→ [待審批] →(核准)→ [活躍]
                          ↓(拒絕)         ↓(撤銷)
                       [已拒絕]        [已禁用]
```

---

## 5. 密鑰管理（D 方案：IndexedDB + 密碼）

### 5.1 設計原則

純網頁實現，**不需要 App**。私鑰永遠以加密形式存於 IndexedDB，只在用戶輸入密碼後才解密到記憶體。

### 5.2 IndexedDB 結構

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
└─ cache_conversations { ... , last_sync }
```

### 5.3 密鑰生命週期

| 密鑰 | 生成位置 | 存儲位置 | 加密方式 | 失效時機 |
|------|--------|--------|--------|--------|
| temp_key | 電腦 | SQLite | 明文 | 使用後立即 |
| device_private_key | 手機 | IndexedDB | 密碼 + AES | 帳號刪除 |
| device_public_key | 手機 | SQLite | 明文（公鑰） | 帳號刪除 |
| session_token | 電腦 | IndexedDB | （JWT 簽名） | 90 天 |
| device_secret | 電腦 | IndexedDB | 隨 IndexedDB | 永久（除非撤銷） |
| refresh_token | 電腦 | IndexedDB | （JWT 簽名） | 2 年 |
| server_private_key | 電腦 | 磁盤 | 檔案系統加密 | 長期保管 |
| dialogue_key | 手機（每次） | 記憶體 | — | 對話結束清空 |

### 5.4 手機關機後的重連流程

```
手機重啟 → 開網頁 → 檢查 IndexedDB
  ├─ 有 private_key → 提示輸入密碼 → 解密 → 自動登入
  │     └─ session 過期？→ 用 refresh_token 自動換新
  └─ 無 private_key → 提示重新掃 QR Code
```

> **重點**：IndexedDB 資料在關機後保留，使用者只需輸入密碼即可恢復，**不需要每次重掃 QR Code**。

---

## 6. 端到端加密流程

### 6.1 手機端加密（發送）

```
明文訊息
  → 生成本次 dialogue_key (隨機 AES 密鑰)
  → AES-256-GCM 加密訊息 (ciphertext + nonce + tag)
  → HMAC-SHA256 簽名 (用 device_secret)
  → RSA 加密 dialogue_key (用伺服器 E2E 公鑰)
  → 組裝請求 → HTTPS POST /chat
```

### 6.2 Cloudflare 可見性

| 欄位 | Cloudflare 能看到？ |
|------|:---:|
| session_token | 可見（無法驗證，簽名密鑰不同） |
| ciphertext | ✗ 完全密文 |
| encrypted_key | ✗ 完全密文 |
| nonce / timestamp | 可見（無意義） |

### 6.3 電腦端解密（接收）

```
驗證 session_token (JWT)
  → 查 device_secret → 驗證 HMAC 簽名
  → RSA 解密 dialogue_key (用伺服器私鑰)
  → AES 解密訊息 → 得明文
  → 權限檢查 → 審計日誌 → 轉發 llama.cpp (本機明文)
```

### 6.4 威脅模型

| 攻擊情境 | 結果 |
|------|------|
| Cloudflare 被入侵 | ✗ 無法解密任何對話（密鑰不在中間） |
| 電腦被入侵 | ✗ 無法解密手機訊息（手機公鑰加密） / ⚠️ 可讀 device_secret，需定期審查設備 |
| IndexedDB 被導出 | ✗ 無密碼無法解密私鑰 |
| XSS 攻擊 | ⚠️ 記憶體中明文私鑰有風險 → 需 CSP + SRI + 密碼門檻 |

---

## 7. Token 計費規則

| 情境 | 計費規則 |
|------|------|
| 正常請求 | prompt_tokens + completion_tokens，實時追蹤 |
| **權限不足被拒** | **不計費** |
| **編輯自己的 prompt** | **新舊都算**（例：舊 10 + 新 25 = 35 tokens） |
| **推理失敗（伺服器錯誤）** | **不計費** |

### 計費精度

- 採**實時追蹤**：每次推理完成後立即寫入 `token_usage`
- 記錄維度：account_id × date × tokens_used，並累計月度用量與限額

---

## 8. 資源限制（優先級隊列）

```
多用戶同時請求
  → 進入優先級隊列
  → 依規則排序（建議：操作類型 + 時間）
  → 逐一放行到 llama.cpp
```

> 優先級規則待最終確認（可選：權限級別 / 操作類型 / FIFO）。
> 配合 Token 額度檢查，防止單一用戶無限制佔用資源。

---

## 9. 審計日誌

### 9.1 記錄欄位（完整版）

| 欄位 | 說明 |
|------|------|
| account_id | 操作者 |
| operation | read / write / edit / delete |
| resource | 目標資源 |
| **ip_address** | 來源 IP |
| **device_id** | 設備識別 |
| **修改前後內容** | 編輯操作的 before / after |
| **完整請求/響應** | 完整 payload（加密存儲） |
| timestamp | 時間戳 |
| status | success / denied |
| reason | 失敗原因 |

### 9.2 保留與存取政策

- **保留期限**：永久保存
- **刪除權限**：只由電腦端管理員刪除
- **查詢權限**：僅電腦端管理員可查（手機端一律無法存取）

---

## 10. 資料存儲結構（SQLite）

```sql
-- QR Code（一次性密鑰）
CREATE TABLE qr_codes (
    qr_code_id    TEXT PRIMARY KEY,
    temp_key      TEXT UNIQUE,
    account_id    TEXT,
    generated_at  TIMESTAMP,
    expires_at    TIMESTAMP,
    used          BOOLEAN DEFAULT FALSE,
    used_at       TIMESTAMP,
    used_by_device TEXT
);

-- 帳號
CREATE TABLE accounts (
    account_id       TEXT PRIMARY KEY,
    username         TEXT,
    device_id        TEXT,
    public_key_phone TEXT,
    permission_level TEXT DEFAULT 'L2',
    status           TEXT DEFAULT 'pending_approval',
    created_at       TIMESTAMP,
    approved_at      TIMESTAMP,
    approved_by      TEXT,
    last_login       TIMESTAMP
);

-- Session
CREATE TABLE sessions (
    session_id    TEXT PRIMARY KEY,
    account_id    TEXT,
    device_id     TEXT,
    session_token TEXT UNIQUE,
    device_secret TEXT,
    refresh_token TEXT,
    created_at    TIMESTAMP,
    expires_at    TIMESTAMP,
    last_activity TIMESTAMP,
    status        TEXT,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

-- 對話（加密存儲）
CREATE TABLE conversations (
    conv_id           TEXT PRIMARY KEY,
    account_id        TEXT,
    user_message      TEXT,   -- 加密
    ai_response       TEXT,   -- 加密
    prompt_tokens     INT,
    completion_tokens INT,
    total_tokens      INT,
    timestamp         TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

-- Token 計費
CREATE TABLE token_usage (
    usage_id      TEXT PRIMARY KEY,
    account_id    TEXT,
    date          DATE,
    tokens_used   INT,
    tokens_limit  INT DEFAULT 10000,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

-- 審計日誌（永久保存）
CREATE TABLE audit_logs (
    log_id        TEXT PRIMARY KEY,
    account_id    TEXT,
    operation     TEXT,
    resource      TEXT,
    ip_address    TEXT,
    device_id     TEXT,
    before_content TEXT,
    after_content  TEXT,
    request_data   TEXT,   -- 加密
    response_data  TEXT,   -- 加密
    timestamp     TIMESTAMP,
    status        TEXT,
    reason        TEXT,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);
```

---

## 11. API 端點規劃

| 方法 | 端點 | 權限 | 說明 |
|------|------|:---:|------|
| POST | /auth/register_device | — | 首次註冊（QR Code 後） |
| POST | /auth/refresh | — | 用 refresh_token 換新 session |
| GET  | /conversations | L1+ | 列出自己的對話 |
| GET  | /conversations/{id} | L1+ | 對話詳情 |
| GET  | /conversations/{id}/export | L1+ | 匯出對話 |
| POST | /conversations | L2+ | 新建對話 |
| POST | /conversations/{id}/message | L2+ | 發送 prompt |
| PUT  | /conversations/{id}/message/{msg_id} | L2+ | 編輯 prompt |
| DELETE | /conversations/{id} | L2+ | 刪除對話 |
| DELETE | /conversations/{id}/message/{msg_id} | L2+ | 刪除單則訊息 |
| —    | /admin/* | 僅電腦端 | 統計 / 日誌 / 用戶管理 |

---

## 12. 安全防護清單

```
傳輸層
☐ 全程 HTTPS (TLS 1.3)
☐ 公鑰證書 Pinning
☐ 子資源完整性 (SRI)

應用層
☐ 端到端加密 (RSA + AES-256-GCM)
☐ HMAC-SHA256 請求簽名（防竄改）
☐ 嚴格 Content Security Policy (CSP)

密鑰
☐ 私鑰 IndexedDB + 密碼（PBKDF2 + AES）
☐ 隨機 Salt 防字典攻擊
☐ 密碼錯誤 N 次鎖定
☐ 定期審查設備列表（撤銷可疑 device_secret）

權限
☐ 每個請求都檢查權限（P4）
☐ 別人的資料一律拒絕
☐ 系統資料僅電腦端可查
```

---

## 13. 實施順序建議

```
階段一（核心）
  1. 代理層骨架（Port 8081，轉發 llama.cpp）
  2. SQLite schema 建立
  3. QR Code 生成 + 一次性失效機制

階段二（認證）
  4. /auth/register_device 流程
  5. 電腦端核准 UI + 權限設定
  6. JWT session + refresh_token

階段三（加密）
  7. 手機端 IndexedDB + 密碼 + 私鑰管理
  8. 端到端加密收發
  9. HMAC 簽名驗證

階段四（業務）
  10. 對話 CRUD + 權限檢查
  11. Token 實時計費
  12. 審計日誌（完整版）

階段五（強化）
  13. 優先級隊列
  14. 自動同步
  15. 安全加固（CSP / SRI / Pinning）
```

---

## 14. 待最終確認事項

| 項目 | 狀態 |
|------|------|
| 優先級隊列的排序規則 | ⏳ 待定（建議：操作類型 + FIFO） |
| Token 月度限額數值 | ⏳ 待定（範例設 10,000） |
| 密碼最小強度要求 | ⏳ 待定（建議 8+ 字符） |
| 密碼錯誤鎖定次數 | ⏳ 待定（建議 5 次） |
| L3 在 EyeSeeMore 的額外功能 | ⏳ ESM 階段再定義 |

---

*本文件為架構設計藍圖，作為 EyeSeeMore 的多用戶加密存取參考範例。*
