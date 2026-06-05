# Token 計費與資源限制

> **目前狀態**：⏳ 待實作（架構設計完成）

## 計費規則

| 情境 | 計費規則 |
|------|------|
| 正常請求 | `prompt_tokens + completion_tokens`，實時追蹤 |
| 權限不足被拒 | **不計費** |
| 編輯自己的 prompt | **新舊都算**（例：舊 10 + 新 25 = 35 tokens）|
| 推理失敗（伺服器錯誤） | **不計費** |

## 計費精度

- **實時追蹤**：每次推理完成後立即寫入 `token_usage` 表
- **記錄維度**：`account_id × date × tokens_used`
- **限額**：預設每日 10,000 tokens（可調整）

## 資料庫記錄方式

```sql
-- 每次推理完成後執行
INSERT OR REPLACE INTO token_usage (usage_id, account_id, date, tokens_used, tokens_limit)
VALUES (?, ?, DATE('now'), ?, 10000)
ON CONFLICT(account_id, date) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used;

-- 同時更新對話記錄
UPDATE conversations SET
  prompt_tokens = ?,
  completion_tokens = ?,
  total_tokens = ?
WHERE conv_id = ?;
```

## 資源限制（優先級隊列）

```
多用戶同時請求
  → 進入優先級隊列
  → 依規則排序（建議：操作類型 + 時間）
  → 逐一放行到 llama.cpp
  → 同時檢查 token 額度（超限拒絕，不計費）
```

優先級排序規則（待確認，可選擇其一）：
- 依 `permission_level`（L3 > L2 > L1）
- 依操作類型（讀 > 寫）
- FIFO（先進先出）

## token_usage 表結構

```
usage_id    TEXT  PRIMARY KEY
account_id  TEXT  (每帳號每日一筆)
date        DATE
tokens_used INT   (累計用量)
tokens_limit INT  DEFAULT 10000
```

→ 完整 Schema 見 [06-database.md](06-database.md)
→ 審計日誌格式見 [01-architecture.md](01-architecture.md)
