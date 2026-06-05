# Go 代理層認證版本 — 使用指南

## 🚀 快速開始

### Step 1：備份舊文件

```powershell
cd D:\software\llama
cp main.go main.go.bak
cp go.mod go.mod.bak
```

### Step 2：替換新文件

```powershell
# 複製新的 main.go（認證版本）
cp main_auth.go main.go

# 複製新的 go.mod
cp go_mod_auth.mod go.mod
```

### Step 3：下載新依賴

```powershell
go mod download
# 或
go mod tidy
```

### Step 4：運行

```powershell
go run main.go
```

**應該看到：**
```
llama.cpp 代理層啟動（含認證）
✓ SQLite 連接成功
✓ 資料庫表格建立完成
🚀 代理層監聽在 :8081
```

---

## 📋 認證流程

### **流程圖**

```
手機掃 QR Code
    ↓
GET /auth/register?temp_key=xxx&account_id=xxx
    ↓ [驗證 temp_key]
    ├─ 有效？ → 標記為已使用
    ├─ 無效或過期？ → 拒絕
    └─ 已使用過？ → 拒絕
    ↓
[生成 JWT session_token]
（90 天有效期）
    ↓
返回 session_token 給手機
    ↓
手機儲存 session_token（IndexedDB）
    ↓
之後每個請求都在 Authorization header 帶上 token
Authorization: Bearer <session_token>
```

---

## 🔌 API 端點（認證版）

### **1. 生成 QR Code（電腦端調用）**

```bash
POST http://127.0.0.1:8081/admin/generate-qr
Content-Type: application/json

{
  "account_id": "user_001"
}
```

**回應：**
```json
{
  "status": "success",
  "temp_key": "temp_abc123...",
  "account_id": "user_001",
  "qr_code_file": "qrcode_user_001.png",
  "expires_in_sec": 3600
}
```

### **2. 註冊設備（手機掃 QR Code 後）**

```bash
POST http://127.0.0.1:8081/auth/register
Content-Type: application/json

{
  "account_id": "user_001",
  "temp_key": "temp_abc123...",
  "device_id": "iPhone-UUID-12345",
  "public_key_phone": "-----BEGIN PUBLIC KEY-----..."
}
```

**回應（成功）：**
```json
{
  "status": "success",
  "session_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**回應（失敗）：**
```json
{
  "status": "error",
  "message": "temp_key 已被使用"
}
```

### **3. 驗證 Token**

```bash
GET http://127.0.0.1:8081/auth/verify
Authorization: Bearer <session_token>
```

**回應：**
```json
{
  "status": "valid",
  "account_id": "user_001",
  "device_id": "iPhone-UUID-12345",
  "permission_level": "L2",
  "expires_at": "2026-09-02 11:15:05"
}
```

### **4. 代理到 llama.cpp（需要認證）**

```bash
# 帶 token 訪問（推薦）
GET http://127.0.0.1:8081/
Authorization: Bearer <session_token>

# 或不帶 token（測試用，生產應拒絕）
GET http://127.0.0.1:8081/
```

### **5. 查看審計日誌（電腦端）**

```bash
GET http://127.0.0.1:8081/admin/logs
```

**回應：**
```json
{
  "status": "success",
  "logs": [
    {
      "log_id": "id_...",
      "account_id": "user_001",
      "operation": "register_device",
      "resource": "device",
      "timestamp": "2026-06-05 11:15:05",
      "status": "success"
    }
  ]
}
```

---

## 🧪 完整測試流程

### **在 PowerShell 中執行**

```powershell
# 1. 生成 QR Code
$body = @{account_id="test_user"} | ConvertTo-Json
$qrResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8081/admin/generate-qr" `
  -Method POST `
  -ContentType "application/json" `
  -Body $body
  
$qrData = $qrResponse.Content | ConvertFrom-Json
$tempKey = $qrData.temp_key
$accountId = $qrData.account_id
Write-Host "✓ QR Code 生成成功"
Write-Host "  Temp Key: $tempKey"

# 2. 註冊設備
$registerBody = @{
  account_id = $accountId
  temp_key = $tempKey
  device_id = "PowerShell-Test-Device"
  public_key_phone = "test-public-key"
} | ConvertTo-Json

$registerResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8081/auth/register" `
  -Method POST `
  -ContentType "application/json" `
  -Body $registerBody

$registerData = $registerResponse.Content | ConvertFrom-Json
$sessionToken = $registerData.session_token
Write-Host "✓ 設備註冊成功"
Write-Host "  Session Token: $($sessionToken.Substring(0, 50))..."

# 3. 驗證 Token
$headers = @{
  "Authorization" = "Bearer $sessionToken"
}
$verifyResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8081/auth/verify" `
  -Headers $headers

Write-Host "✓ Token 驗證成功"
Write-Host $verifyResponse.Content

# 4. 訪問 llama.cpp（帶 token）
$llamaResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8081/" `
  -Headers $headers

Write-Host "✓ 成功轉發到 llama.cpp"
Write-Host "  狀態碼: $($llamaResponse.StatusCode)"
```

---

## 🔐 安全特性

### **已實現**
✅ temp_key 一次性使用（使用後立即標記）  
✅ temp_key 1 小時有效期  
✅ JWT session_token（90 天有效期）  
✅ HMAC-SHA256 簽名（防竄改）  
✅ 完整審計日誌  

### **待實現（階段 3）**
⏳ 端到端加密（RSA + AES）  
⏳ 權限檢查（L1/L2/L3）  
⏳ Token 計費（實時追蹤）  
⏳ 速率限制（優先級隊列）  

---

## 🐛 常見問題

### **Q1：「找不到 JWT 庫」**

```powershell
go get github.com/golang-jwt/jwt/v5
go mod tidy
```

### **Q2：「temp_key 已被使用」**

這是正常的！同一個 temp_key 只能使用一次。電腦端需要生成新的 QR Code。

### **Q3：「token 無效」**

檢查：
1. Authorization header 格式是否正確（`Bearer <token>`）
2. token 是否過期（90 天）
3. JWT_SECRET 是否一致

### **Q4：手機如何儲存 token？**

在下一階段，會加入 IndexedDB + 密碼保護的邏輯（D 方案）。目前手機端需要：
1. 從 QR Code 取得 temp_key
2. 調用 `/auth/register` 註冊
3. 得到 session_token
4. 存到 IndexedDB（加密）
5. 每次請求帶上 token

---

## 📚 代碼亮點

### **JWT 生成**
```go
func generateJWT(accountID, deviceID, permissionLevel string) (string, error) {
    expiresAt := time.Now().Add(SESSION_DURATION)
    claims := &Claims{
        AccountID: accountID,
        DeviceID: deviceID,
        PermissionLevel: permissionLevel,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(expiresAt),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(JWT_SECRET))
}
```

### **Token 驗證**
```go
func verifyJWT(tokenString string) (*Claims, error) {
    claims := &Claims{}
    token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
        return []byte(JWT_SECRET), nil
    })
    if err != nil {
        return nil, err
    }
    if !token.Valid {
        return nil, fmt.Errorf("token 無效")
    }
    return claims, nil
}
```

---

## 🎯 下一步

現在已完成：
✅ QR Code 一次性驗證  
✅ JWT Session Token（90 天）  
✅ 審計日誌  

下一步（階段 3）：
⏳ 手機端密鑰儲存（IndexedDB + 密碼）  
⏳ 端到端加密（RSA + AES）  
⏳ 權限檢查中間件（L1/L2/L3）  
⏳ Token 計費系統  

---

*認證版本完成！下次升級會加入加密邏輯。*
