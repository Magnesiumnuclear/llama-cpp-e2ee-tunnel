# 權限系統（L1 / L2 / L3）

## 三級權限定義

| 權限 | 名稱 | 對話讀 | 對話寫 | 對話改 | 對話刪 |
|------|------|:---:|:---:|:---:|:---:|
| **L1** | 唯讀 | ✓ | ✗ | ✗ | ✗ |
| **L2** | 讀寫改刪（**預設**） | ✓ | ✓ | ✓ | ✓ |
| **L3** | 完整存取 | ✓ | ✓ | ✓ | ✓ |

> **預設**：新帳號未經設定時為 **L2**。
> L3 目前同 L2，保留供 EyeSeeMore 擴充使用。

## 權限邊界

```
【自己的對話】
讀取對話歷史              L1 ✓  L2 ✓  L3 ✓
新建對話 / 發送 prompt    L1 ✗  L2 ✓  L3 ✓
編輯自己的 prompt         L1 ✗  L2 ✓  L3 ✓
刪除自己的對話            L1 ✗  L2 ✓  L3 ✓
匯出對話（CSV/PDF）       L1 ✓  L2 ✓  L3 ✓

【別人的對話】
任何操作                  一律 ✗（隱私保護，永不允許）

【系統資料（僅電腦端）】
Token 統計 / 系統日誌
用戶列表 / 權限配置       一律 ✗（從手機端存取）
```

## L1 無資料時的回應格式

```json
{
  "status": "success",
  "data": [],
  "message": "無資料"
}
```

## 程式碼實作（main.go）

```go
// 權限等級對應
levels := map[string]int{"L1": 1, "L2": 2, "L3": 3}

// 檢查：userLevel >= requiredLevel 才放行
func checkPermission(userLevel, requiredLevel string) bool {
    userLv := levels[userLevel]
    requiredLv := levels[requiredLevel]
    return userLv >= requiredLv
}
```

中間件從 JWT Claims 取出 `permission` 欄位，注入到：
- `X-Permission` Header → 供後續 Handler 使用

## 帳號狀態對權限的影響

| 帳號狀態 | 能存取 API？ |
|----------|:-----------:|
| `pending_approval` | ✗ |
| `active` | ✓（依 L1/L2/L3）|
| `disabled` / `rejected` | ✗ |

→ Token 計費規則見 [05-token-billing.md](05-token-billing.md)
