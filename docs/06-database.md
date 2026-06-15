# SQLite 資料庫 Schema

資料庫檔案：`./llama-proxy.db`（WAL 模式）

## 六張資料表

### 1. qr_codes — 一次性密鑰（掃碼註冊 + 重新登入共用）

```sql
CREATE TABLE IF NOT EXISTS qr_codes (
    qr_code_id     TEXT PRIMARY KEY,
    temp_key       TEXT UNIQUE,           -- 註冊用 temp_key 或重新登入用 code
    account_id     TEXT,
    generated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at     TIMESTAMP,             -- 註冊：生成後 1 小時；重新登入：5 分鐘
    used           BOOLEAN DEFAULT 0,     -- 使用後立即設為 1（單次使用）
    used_at        TIMESTAMP,
    used_by_device TEXT,
    kind           TEXT DEFAULT 'register' -- 'register'（掃碼註冊）/ 'relogin'（重新登入）/ 'e2e'（E2E 測試憑證交換）
);
```

> `kind` 用來區分一次性 code 的用途，各流程只接受自己的 kind（`/auth/register`→`register`、`/auth/relogin`→`relogin`、`/e2e-test/exchange`→`e2e`），避免 code 被跨流程重放（例如重新登入 code 被拿去註冊流程把 active 帳號打回待審）。舊資料庫由啟動時的 `ALTER TABLE` 自動補上（預設 `register`）。

### 2. accounts — 帳號與設備資訊

```sql
CREATE TABLE IF NOT EXISTS accounts (
    account_id       TEXT PRIMARY KEY,
    username         TEXT,
    device_id        TEXT,
    device_secret    TEXT,                -- HMAC 簽名用
    permission_level TEXT DEFAULT 'L2',   -- L1 / L2 / L3
    status           TEXT DEFAULT 'pending_approval',
    -- status 可為: pending_approval / active / disabled / rejected
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    approved_at      TIMESTAMP,
    last_login       TIMESTAMP,           -- 每次「重新登入」(/auth/relogin) 成功時更新
    tokens_valid_after INTEGER DEFAULT 0  -- JWT 撤銷：簽發時間(iat)早於此 Unix 秒數的 token 一律失效（0＝未撤銷）
);
```

### 3. sessions — JWT Token 管理

```sql
CREATE TABLE IF NOT EXISTS sessions (
    session_id    TEXT PRIMARY KEY,
    account_id    TEXT,
    device_id     TEXT,
    session_token TEXT UNIQUE,   -- JWT，90 天有效
    refresh_token TEXT UNIQUE,   -- JWT，2 年有效
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at    TIMESTAMP,
    last_activity TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);
```

### 4. conversations — 對話記錄

```sql
CREATE TABLE IF NOT EXISTS conversations (
    conv_id           TEXT PRIMARY KEY,
    account_id        TEXT,
    user_message      TEXT,               -- 未來加密存儲
    ai_response       TEXT,               -- 未來加密存儲
    prompt_tokens     INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_tokens      INTEGER DEFAULT 0,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);
```

### 5. token_usage — 每日 Token 用量

```sql
CREATE TABLE IF NOT EXISTS token_usage (
    usage_id     TEXT PRIMARY KEY,
    account_id   TEXT,
    date         DATE,
    tokens_used  INTEGER DEFAULT 0,
    tokens_limit INTEGER DEFAULT 10000,   -- 每日上限
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);
```

### 6. audit_logs — 永久審計日誌

```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    log_id        TEXT PRIMARY KEY,
    account_id    TEXT,
    operation     TEXT,       -- register_device / generate_qr / chat / approve ...
    resource      TEXT,       -- qr_code / account / conversation ...
    ip_address    TEXT,
    device_id     TEXT,
    request_data  TEXT,       -- 未來加密存儲
    response_data TEXT,       -- 未來加密存儲
    status        TEXT,       -- success / denied
    reason        TEXT,
    timestamp     TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## 資料庫 Migration

啟動時 `initDB()` 會自動對舊資料庫執行 `ALTER TABLE` 補齊缺少的欄位，**不需要手動操作或删除資料庫**。欄位已存在時 SQLite 回傳錯誤會被安全忽略。

目前轉移項目：`audit_logs.ip_address`、`audit_logs.device_id`、`audit_logs.request_data`、`audit_logs.response_data`

## 保留與存取政策

| 表 | 保留期限 | 查詢權限 |
|----|---------|---------|
| audit_logs | **永久** | 僅電腦端管理員 |
| conversations | 依帳號設定 | 僅帳號本人 |
| token_usage | 長期 | 管理員 + 本人 |
| sessions | 過期後可清理 | 管理員 |
