# Go 代理層運行指南

## 📋 前置要求

1. **Go 已安裝**：`go version` 能顯示版本
2. **llama.cpp 正在運行**：http://127.0.0.1:8080 可訪問
3. **Windows PowerShell** 或 CMD

---

## 🚀 快速啟動（5 分鐘）

### **Step 1：下載依賴**

```bash
# 進入專案目錄
cd C:\llama-proxy

# 下載 Go 依賴（自動執行）
go mod download

# 或者更新
go get -u github.com/mattn/go-sqlite3
go get -u github.com/skip2/go-qrcode
```

### **Step 2：編譯並運行**

```bash
# 方式 A：直接運行（開發模式）
go run main.go

# 方式 B：編譯成 .exe（生產模式）
go build -o llama-proxy.exe main.go
.\llama-proxy.exe
```

**應該看到：**
```
==================================================
llama.cpp 代理層啟動
==================================================
2024/06/24 10:30:15 ✓ SQLite 連接成功
2024/06/24 10:30:15 ✓ 資料庫表格建立完成
2024/06/24 10:30:15 🚀 代理層監聽在 :8081
2024/06/24 10:30:15    健康檢查: http://127.0.0.1:8081/health
2024/06/24 10:30:15    生成 QR Code: POST http://127.0.0.1:8081/admin/generate-qr
2024/06/24 10:30:15    查看日誌: http://127.0.0.1:8081/admin/logs
```

✅ 成功！代理層正在運行

---

## 🧪 測試端點

### **1. 健康檢查**

```bash
curl http://127.0.0.1:8081/health
```

回應：
```json
{
  "status": "ok",
  "message": "代理層正常運行"
}
```

### **2. 生成 QR Code**

```bash
# PowerShell
$body = @{account_id="user_001"} | ConvertTo-Json
Invoke-WebRequest -Uri "http://127.0.0.1:8081/admin/generate-qr" `
  -Method POST `
  -ContentType "application/json" `
  -Body $body

# 或用 curl
curl -X POST http://127.0.0.1:8081/admin/generate-qr `
  -H "Content-Type: application/json" `
  -d "{\"account_id\":\"user_001\"}"
```

回應：
```json
{
  "status": "success",
  "temp_key": "temp_1719321615000000000",
  "account_id": "user_001",
  "qr_code_file": "qrcode_user_001.png"
}
```

✅ 會在專案目錄生成 `qrcode_user_001.png`

### **3. 查看審計日誌**

```bash
curl http://127.0.0.1:8081/admin/logs
```

### **4. 代理到 llama.cpp**

```bash
# 訪問代理層的 llama Web UI
http://127.0.0.1:8081/

# 或發送 prompt 到代理層（自動轉發到 llama.cpp）
curl http://127.0.0.1:8081/api/...
```

---

## 📚 Go 代碼解說（學習重點）

### **1. 包和導入**

```go
package main

import (
    "database/sql"  // SQLite 驅動
    "net/http"      // HTTP 伺服器
    _ "github.com/mattn/go-sqlite3"  // 下劃線 = 只執行初始化
)
```

**Python 對標**：
```python
import sqlite3
import http.server
```

### **2. 變量聲明**

```go
var db *sql.DB              // 全局變量（大寫 = 可匯出）

func main() {
    tempKey := "abc123"     // 短聲明（:=），只能在函數內
    var accountID string    // 標準聲明
}
```

**Python 對標**：
```python
db = None  # 全局

def main():
    temp_key = "abc123"     # 隱式聲明
    account_id: str = ""    # 型別提示
```

### **3. 錯誤處理（Go 特色）**

```go
// Go 的錯誤處理方式
if err := initDB(); err != nil {
    log.Fatalf("❌ 初始化失敗: %v\n", err)
}

// Python 對標
try:
    init_db()
except Exception as err:
    log.fatal(f"❌ 初始化失敗: {err}")
```

### **4. HTTP 路由**

```go
// Go：簡潔
http.HandleFunc("/health", healthCheck)
http.ListenAndServe(":8081", nil)

// Python (Flask) 對標
@app.route('/health')
def health_check():
    return {...}

if __name__ == '__main__':
    app.run(port=8081)
```

### **5. 並發（Go 優勢）**

```go
// Go：輕鬆處理 1000 個並發
for i := 0; i < 1000; i++ {
    go handleRequest()  // goroutine：極輕量
}

// Python：需要複雜的 async/await
import asyncio
async def handle_request():
    await asyncio.sleep(0)
```

---

## 🔧 常見問題

### **Q1：「找不到模組」錯誤**

```
cannot find package
```

**解決**：
```bash
go mod tidy
go mod download
```

### **Q2：Port 8081 已被佔用**

```go
// 修改 main.go 的這一行
if err := http.ListenAndServe(":8082", nil); err != nil {  // 改成 8082
```

### **Q3：llama.cpp 無法連接**

確保：
1. llama.cpp 在 8080 正常運行
2. 代理層能 ping 到 127.0.0.1:8080
3. Windows 防火牆沒有擋住

### **Q4：SQLite 權限錯誤**

```
database is locked
```

**解決**：刪除 `llama-proxy.db`，重新運行

---

## 📈 下一步：逐步實現功能

### **現在完成的（階段 1）**
✅ 代理層骨架  
✅ SQLite 初始化  
✅ QR Code 生成  
✅ 轉發到 llama.cpp  

### **接下來要做（階段 2-3）**

1. **認證流程**
   ```go
   // 驗證 QR Code 有效性
   // 檢查 temp_key 是否已使用
   ```

2. **權限檢查**
   ```go
   // 檢查 session_token
   // 驗證 L1/L2/L3 權限
   ```

3. **端到端加密**
   ```go
   // RSA 解密手機發送的數據
   // AES 加密回應
   ```

4. **Token 計費**
   ```go
   // 記錄每個請求的 token 消耗
   ```

---

## 💡 Go vs Python 的感受

| 方面 | Go | Python |
|------|-----|---------|
| 啟動速度 | 毫秒級 | 1-2 秒 |
| 記憶體 | ~10MB | ~50MB |
| 並發能力 | 輕鬆 1000+ | 需多進程 |
| 學習曲線 | 陡但快 | 平緩 |
| 代碼量 | 少（~300 行） | 多（~500 行） |

---

## 📞 Debug 技巧

### **1. 增加日誌**

```go
log.Printf("Debug: accountID=%s, tempKey=%s\n", accountID, tempKey)
```

### **2. 查看 SQLite 資料庫**

```bash
# 安裝 sqlite3 CLI
choco install sqlite

# 查看資料庫內容
sqlite3 llama-proxy.db
> SELECT * FROM qr_codes;
> SELECT * FROM audit_logs;
```

### **3. 查看 proxy.log**

```bash
# 監控日誌
Get-Content -Path "proxy.log" -Tail 20 -Wait
```

---

## ✨ 你現在掌握了

✅ Go 基礎語法  
✅ HTTP 伺服器（比 Python Flask 簡單）  
✅ SQLite 操作  
✅ 反向代理原理  
✅ 如何編譯 Go 成 .exe

**下次可以嘗試**：
- 加入 JWT 驗證（session_token）
- 實現權限檢查
- 加密/解密邏輯

---

## 🎯 任務檢查清單

- [ ] Go 已安裝
- [ ] 進入 C:\llama-proxy
- [ ] go mod download 完成
- [ ] go run main.go 成功運行
- [ ] curl /health 回應正常
- [ ] 生成 QR Code 成功
- [ ] llama.cpp 能通過代理層訪問

✅ 全部完成？恭喜！你已經掌握了 Go 基礎。

---

*下一步準備好時，我們會加入認證邏輯。*
