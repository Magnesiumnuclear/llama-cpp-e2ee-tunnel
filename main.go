package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/skip2/go-qrcode"
)

// ============================================================
// 全局變量
// ============================================================

var (
	db               *sql.DB
	serverSecret     []byte          // JWT 簽名密鑰
	serverE2EPrivKey *rsa.PrivateKey // E2E RSA-2048 私鑰（啟動時載入或自動生成）
	mu               sync.Mutex
	publicURL        string // 對外公網 URL（Cloudflare Tunnel）
)

// ============================================================
// 資料結構
// ============================================================

// API 回應格式
type APIResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// JWT Claims
type CustomClaims struct {
	AccountID  string `json:"account_id"`
	DeviceID   string `json:"device_id"`
	Permission string `json:"permission"`
	jwt.RegisteredClaims
}

// E2E 加密請求格式（手機端發送，伺服器解密）
type E2ERequest struct {
	EncryptedKey  string `json:"encrypted_key"`  // base64: RSA-OAEP 加密的 AES-256 dialogue_key
	Ciphertext    string `json:"ciphertext"`     // base64: AES-256-GCM 密文（末尾含 16-byte GCM tag）
	Nonce         string `json:"nonce"`          // base64: 12-byte AES-GCM nonce
	HMACSignature string `json:"hmac_signature"` // base64: HMAC-SHA256（以 device_secret 為密鑰）
}

// ============================================================
// 工具函數
// ============================================================

// 生成安全隨機字符串
func generateSecureKey(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("警告: 隨機數生成失敗, 使用時間戳: %v\n", err)
		return fmt.Sprintf("fallback_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// 生成唯一 ID
func generateID(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, generateSecureKey(8))
}

// 生成 JWT Token
func generateJWT(accountID, deviceID, permission string, duration time.Duration) (string, error) {
	claims := CustomClaims{
		AccountID:  accountID,
		DeviceID:   deviceID,
		Permission: permission,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        generateID("jwt"),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(serverSecret)
}

// 驗證 JWT Token
func validateJWT(tokenString string) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("無效的簽名方式: %v", token.Header["alg"])
		}
		return serverSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("無效的 Token")
}

// 發送 JSON 回應
func sendJSON(w http.ResponseWriter, statusCode int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// E2E 加密 — RSA 密鑰管理與解密
// ============================================================

// loadOrGenerateRSAKeyPair 啟動時自動載入或生成伺服器 RSA-2048 密鑰對。
// 私鑰以 PKCS#8 PEM 格式存於 server_private.pem（mode 0600，僅擁有者可讀）。
// 公鑰存於 server_public.pem（mode 0644），同時透過 GET /api/public-key 提供給前端。
func loadOrGenerateRSAKeyPair() error {
	const privPath = "server_private.pem"
	const pubPath = "server_public.pem"

	// 嘗試從磁碟載入已存在的私鑰
	if data, err := os.ReadFile(privPath); err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
			if parseErr == nil {
				if rsaKey, ok := key.(*rsa.PrivateKey); ok {
					serverE2EPrivKey = rsaKey
					log.Println("✓ RSA E2E 私鑰已從磁碟載入")
					return nil
				}
			}
			log.Printf("⚠️  RSA 私鑰解析失敗，重新生成: %v\n", parseErr)
		}
	}

	// 生成新的 RSA-2048 密鑰對
	log.Println("🔑 生成 RSA-2048 E2E 密鑰對...")
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("RSA 密鑰生成失敗: %w", err)
	}

	// 序列化私鑰（PKCS#8 DER → PEM）並寫入磁碟（mode 0600 僅擁有者可讀）
	privDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("私鑰序列化失敗: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return fmt.Errorf("私鑰寫入 %s 失敗: %w", privPath, err)
	}

	// 序列化公鑰（SPKI DER → PEM）並寫入磁碟
	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return fmt.Errorf("公鑰序列化失敗: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return fmt.Errorf("公鑰寫入 %s 失敗: %w", pubPath, err)
	}

	serverE2EPrivKey = privKey
	log.Printf("✓ RSA E2E 密鑰對已生成並儲存（%s / %s）\n", privPath, pubPath)
	return nil
}

// decryptE2ERequest 解密手機端發來的 E2E 加密請求，回傳明文 JSON bytes。
//
// 安全流程（必須全部通過）：
//  1. Base64 解碼各欄位
//  2. HMAC-SHA256 驗證（以 deviceSecret 為密鑰，constant-time 比較，防止竄改與 timing attack）
//  3. RSA-OAEP（SHA-256）解密 encrypted_key，取得一次性 AES-256 dialogue_key
//  4. AES-256-GCM 解密 ciphertext（GCM tag 驗證確保密文完整性），取得明文 JSON
func decryptE2ERequest(e2eReq E2ERequest, deviceSecret string) ([]byte, error) {
	// 1. Base64 解碼各欄位
	encKey, err := base64.StdEncoding.DecodeString(e2eReq.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("encrypted_key base64 解碼失敗: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(e2eReq.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("ciphertext base64 解碼失敗: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(e2eReq.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce base64 解碼失敗: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(e2eReq.HMACSignature)
	if err != nil {
		return nil, fmt.Errorf("hmac_signature base64 解碼失敗: %w", err)
	}

	// 2. HMAC-SHA256 驗證
	// 簽名對象：base64(encrypted_key) + "." + base64(nonce) + "." + base64(ciphertext)
	// 此格式與前端 Web Crypto API 實作完全一致，任意欄位被竄改都會驗證失敗
	mac := hmac.New(sha256.New, []byte(deviceSecret))
	mac.Write([]byte(e2eReq.EncryptedKey + "." + e2eReq.Nonce + "." + e2eReq.Ciphertext))
	expected := mac.Sum(nil)
	if !hmac.Equal(expected, sig) {
		return nil, fmt.Errorf("HMAC 驗證失敗：請求可能被竄改或 device_secret 不符")
	}

	// 3. RSA-OAEP（SHA-256）解密 dialogue_key
	if serverE2EPrivKey == nil {
		return nil, fmt.Errorf("伺服器 E2E 私鑰尚未初始化")
	}
	dialogueKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, serverE2EPrivKey, encKey, nil)
	if err != nil {
		return nil, fmt.Errorf("RSA-OAEP 解密 dialogue_key 失敗: %w", err)
	}
	if len(dialogueKey) != 32 {
		return nil, fmt.Errorf("dialogue_key 長度無效（應為 32 bytes，實際 %d bytes）", len(dialogueKey))
	}

	// 4. AES-256-GCM 解密（GCM tag 自動驗證密文完整性）
	block, err := aes.NewCipher(dialogueKey)
	if err != nil {
		return nil, fmt.Errorf("AES cipher 初始化失敗: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM 初始化失敗: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("nonce 長度無效（應為 %d bytes，實際 %d bytes）", gcm.NonceSize(), len(nonce))
	}
	// ciphertext 末尾 16 bytes 為 GCM authentication tag，gcm.Open 自動驗證並去除
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("AES-256-GCM 解密失敗（密文或 GCM tag 被竄改）: %w", err)
	}

	return plaintext, nil
}

// ============================================================
// 資料庫
// ============================================================

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./llama-proxy.db?_journal_mode=WAL")
	if err != nil {
		return err
	}

	if err = db.Ping(); err != nil {
		return err
	}

	log.Println("✓ SQLite 連接成功")

	schema := `
	CREATE TABLE IF NOT EXISTS qr_codes (
		qr_code_id    TEXT PRIMARY KEY,
		temp_key      TEXT UNIQUE,
		account_id    TEXT,
		generated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at    TIMESTAMP,
		used          BOOLEAN DEFAULT 0,
		used_at       TIMESTAMP,
		used_by_device TEXT
	);

	CREATE TABLE IF NOT EXISTS accounts (
		account_id       TEXT PRIMARY KEY,
		username         TEXT,
		device_id        TEXT,
		device_secret    TEXT,
		permission_level TEXT DEFAULT 'L2',
		status           TEXT DEFAULT 'pending_approval',
		created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		approved_at      TIMESTAMP,
		last_login       TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		session_id    TEXT PRIMARY KEY,
		account_id    TEXT,
		device_id     TEXT,
		session_token TEXT UNIQUE,
		refresh_token TEXT UNIQUE,
		created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at    TIMESTAMP,
		last_activity TIMESTAMP,
		FOREIGN KEY (account_id) REFERENCES accounts(account_id)
	);

	CREATE TABLE IF NOT EXISTS conversations (
		conv_id           TEXT PRIMARY KEY,
		account_id        TEXT,
		user_message      TEXT,
		ai_response       TEXT,
		prompt_tokens     INTEGER DEFAULT 0,
		completion_tokens INTEGER DEFAULT 0,
		total_tokens      INTEGER DEFAULT 0,
		created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (account_id) REFERENCES accounts(account_id)
	);

	CREATE TABLE IF NOT EXISTS token_usage (
		usage_id   TEXT PRIMARY KEY,
		account_id TEXT,
		date       DATE,
		tokens_used INTEGER DEFAULT 0,
		tokens_limit INTEGER DEFAULT 10000,
		FOREIGN KEY (account_id) REFERENCES accounts(account_id)
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		log_id         TEXT PRIMARY KEY,
		account_id     TEXT,
		operation      TEXT,
		resource       TEXT,
		ip_address     TEXT,
		device_id      TEXT,
		request_data   TEXT,
		response_data  TEXT,
		timestamp      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status         TEXT,
		reason         TEXT
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// 自動補齊舊資料庫可能缺少的欄位（忽略「column already exists」錯誤）
	migrations := []string{
		"ALTER TABLE audit_logs ADD COLUMN ip_address TEXT",
		"ALTER TABLE audit_logs ADD COLUMN device_id TEXT",
		"ALTER TABLE audit_logs ADD COLUMN request_data TEXT",
		"ALTER TABLE audit_logs ADD COLUMN response_data TEXT",
	}
	for _, m := range migrations {
		db.Exec(m) // 忽略錯誤：欄位已存在時 SQLite 會回傳錯誤，屬正常情況
	}

	log.Println("✓ 資料庫表格建立完成")
	return nil
}

// ============================================================
// 審計日誌
// ============================================================

func logAudit(accountID, operation, resource, ipAddr, deviceID, status, reason string) {
	logID := generateID("log")
	query := `
	INSERT INTO audit_logs (log_id, account_id, operation, resource, ip_address, device_id, timestamp, status, reason)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, logID, accountID, operation, resource, ipAddr, deviceID, time.Now(), status, reason)
	if err != nil {
		log.Printf("警告: 審計日誌寫入失敗: %v\n", err)
	}
}

// ============================================================
// 認證中間件
// ============================================================

// 從請求中提取 Token
func extractToken(r *http.Request) string {
	// 從 Header 提取：Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	// 從 Cookie 提取
	cookie, err := r.Cookie("session_token")
	if err == nil {
		return cookie.Value
	}

	// 從 URL 參數提取
	return r.URL.Query().Get("token")
}

// 認證中間件：檢查用戶是否已登入
func authMiddleware(next http.HandlerFunc, requiredLevel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 防止 Header 偽造：清除客戶端傳入的內部 Header
		r.Header.Del("X-Account-ID")
		r.Header.Del("X-Device-ID")
		r.Header.Del("X-Permission")

		tokenString := extractToken(r)
		if tokenString == "" {
			sendJSON(w, http.StatusUnauthorized, APIResponse{
				Status: "error",
				Error:  "未提供認證 Token",
			})
			logAudit("unknown", "auth_check", r.URL.Path, r.RemoteAddr, "", "denied", "no_token")
			return
		}

		// 驗證 JWT
		claims, err := validateJWT(tokenString)
		if err != nil {
			sendJSON(w, http.StatusUnauthorized, APIResponse{
				Status: "error",
				Error:  "Token 無效或已過期",
			})
			logAudit("unknown", "auth_check", r.URL.Path, r.RemoteAddr, "", "denied", err.Error())
			return
		}

		// 檢查帳號狀態
		var status string
		err = db.QueryRow("SELECT status FROM accounts WHERE account_id = ?", claims.AccountID).Scan(&status)
		if err != nil || status != "active" {
			sendJSON(w, http.StatusForbidden, APIResponse{
				Status: "error",
				Error:  "帳號未啟用或已被停用",
			})
			logAudit(claims.AccountID, "auth_check", r.URL.Path, r.RemoteAddr, claims.DeviceID, "denied", "account_inactive")
			return
		}

		// 檢查權限
		if !checkPermission(claims.Permission, requiredLevel) {
			sendJSON(w, http.StatusForbidden, APIResponse{
				Status: "error",
				Error:  fmt.Sprintf("權限不足：需要 %s，你的權限是 %s", requiredLevel, claims.Permission),
			})
			logAudit(claims.AccountID, "permission_check", r.URL.Path, r.RemoteAddr, claims.DeviceID, "denied", "insufficient_permission")
			return
		}

		// 更新最後活動時間
		db.Exec("UPDATE sessions SET last_activity = ? WHERE account_id = ? AND device_id = ?",
			time.Now(), claims.AccountID, claims.DeviceID)

		// 將用戶資訊加到 Header（傳給下游處理）
		r.Header.Set("X-Account-ID", claims.AccountID)
		r.Header.Set("X-Device-ID", claims.DeviceID)
		r.Header.Set("X-Permission", claims.Permission)

		next(w, r)
	}
}

// 權限檢查：L1 < L2 < L3
func checkPermission(userLevel, requiredLevel string) bool {
	levels := map[string]int{"L1": 1, "L2": 2, "L3": 3}
	userLv, ok1 := levels[userLevel]
	requiredLv, ok2 := levels[requiredLevel]
	if !ok1 || !ok2 {
		return false
	}
	return userLv >= requiredLv
}

// ============================================================
// HTTP 路由處理
// ============================================================

// --- 公開端點 ---

// 健康檢查
func healthHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "ok",
		Message: "代理層正常運行",
	})
}

// GET /api/public-key — 返回伺服器 RSA E2E 公鑰（SPKI PEM 格式）
// 前端使用此公鑰以 RSA-OAEP 加密每次對話的一次性 dialogue_key（AES-256）
func publicKeyHandler(w http.ResponseWriter, r *http.Request) {
	if serverE2EPrivKey == nil {
		sendJSON(w, http.StatusServiceUnavailable, APIResponse{
			Status: "error",
			Error:  "E2E 密鑰尚未初始化，請稍後再試",
		})
		return
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&serverE2EPrivKey.PublicKey)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "公鑰序列化失敗"})
		return
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data:   map[string]string{"public_key": pubPEM},
	})
}

// 設備註冊（手機掃 QR Code 後調用）
// GET /auth/register?temp_key=xxx&account_id=xxx → 返回 HTML 頁面，頁面自動完成 POST 流程
func registerDeviceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tempKey := r.URL.Query().Get("temp_key")
		accountID := r.URL.Query().Get("account_id")
		if tempKey == "" || accountID == "" {
			http.Error(w, "缺少 temp_key 或 account_id 參數", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>llama-proxy 設備認證</title>
<style>
  body { margin: 0; background: #1a1a2e; color: #e0e0e0; font-family: sans-serif;
         display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .card { background: #16213e; border-radius: 12px; padding: 2rem 2.5rem; max-width: 360px;
          width: 90%%; text-align: center; box-shadow: 0 4px 24px rgba(0,0,0,0.4); }
  h2 { margin: 0 0 1.5rem; font-size: 1.2rem; color: #a0c4ff; }
  #status { font-size: 1rem; line-height: 1.7; }
  .ok  { color: #69f0ae; }
  .err { color: #ff5252; }
  .dim { color: #888; font-size: 0.85rem; margin-top: 1rem; }
</style>
</head>
<body>
<div class="card">
  <h2>llama-proxy 設備認證</h2>
  <div id="status">正在驗證 QR Code...</div>
  <div class="dim" id="hint"></div>
</div>
<script>
(async () => {
  const set = (msg, cls) => {
    document.getElementById('status').innerHTML = msg;
    if (cls) document.getElementById('status').className = cls;
  };
  const hint = (msg) => document.getElementById('hint').textContent = msg;

  const tempKey   = %q;
  const accountId = %q;
  const deviceId  = 'web-' + (navigator.userAgent.replace(/\s+/g,'').slice(-12) || Math.random().toString(36).slice(2));

  try {
    const res = await fetch('/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ temp_key: tempKey, account_id: accountId, device_id: deviceId })
    });
    const data = await res.json();

    if (res.ok && data.status === 'success') {
      set('✅ 設備已連接！<br>等待電腦端核准...', 'ok');
      hint('核准後將自動進入 llama.cpp');
      const deviceSecret = data.data && data.data.device_secret;
      if (!deviceSecret) {
        set('❌ 伺服器未回傳 device_secret，無法繼續', 'err');
        return;
      }
      const poll = async () => {
        try {
          const pr = await fetch('/auth/poll?account_id=' + encodeURIComponent(accountId) + '&device_secret=' + encodeURIComponent(deviceSecret));
          const pd = await pr.json();
          const st = pd.data && pd.data.status;
          if (st === 'approved') {
            set('✅ 核准成功！正在進入...', 'ok');
            hint('');
            setTimeout(() => { window.location.href = '/'; }, 800);
            return;
          } else if (st === 'rejected' || st === 'disabled') {
            set('❌ 此設備已被' + (st === 'rejected' ? '拒絕' : '停用'), 'err');
            return;
          }
        } catch (e) {}
        setTimeout(poll, 2500);
      };
      setTimeout(poll, 2500);
    } else {
      const errMap = {
        'temp_key 和 device_id 為必填': '參數缺失，請重新掃描 QR Code',
        '無效的 temp_key': '❌ QR Code 無效，請重新生成',
        '此 QR Code 已被使用，請重新生成': '❌ QR Code 已被使用<br>請在電腦端重新生成',
        '此 QR Code 已過期，請重新生成': '❌ QR Code 已過期（超過 1 小時）<br>請在電腦端重新生成',
      };
      set(errMap[data.error] || ('❌ 錯誤：' + (data.error || data.message || '未知錯誤')), 'err');
    }
  } catch (e) {
    set('❌ 網路錯誤，請確認代理層正在運行', 'err');
    hint(e.toString());
  }
})();
</script>
</body>
</html>`, tempKey, accountID)
		return
	}

	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 GET 或 POST"})
		return
	}

	var req struct {
		TempKey   string `json:"temp_key"`
		DeviceID  string `json:"device_id"`
		AccountID string `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "無效的請求格式"})
		return
	}

	if req.TempKey == "" || req.DeviceID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "temp_key 和 device_id 為必填"})
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// 查詢 QR Code
	var qrCodeID, accountID string
	var expiresAt time.Time
	var used bool
	err := db.QueryRow(
		"SELECT qr_code_id, account_id, expires_at, used FROM qr_codes WHERE temp_key = ?",
		req.TempKey,
	).Scan(&qrCodeID, &accountID, &expiresAt, &used)

	if err == sql.ErrNoRows {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "無效的 temp_key"})
		logAudit("unknown", "register_device", "qr_code", r.RemoteAddr, req.DeviceID, "denied", "invalid_temp_key")
		return
	}

	// 檢查是否已使用
	if used {
		sendJSON(w, http.StatusConflict, APIResponse{Status: "error", Error: "此 QR Code 已被使用，請重新生成"})
		logAudit(accountID, "register_device", "qr_code", r.RemoteAddr, req.DeviceID, "denied", "qr_already_used")
		return
	}

	// 檢查是否過期
	if time.Now().After(expiresAt) {
		sendJSON(w, http.StatusGone, APIResponse{Status: "error", Error: "此 QR Code 已過期，請重新生成"})
		logAudit(accountID, "register_device", "qr_code", r.RemoteAddr, req.DeviceID, "denied", "qr_expired")
		return
	}

	// 標記 QR Code 為已使用
	db.Exec("UPDATE qr_codes SET used = 1, used_at = ?, used_by_device = ? WHERE qr_code_id = ?",
		time.Now(), req.DeviceID, qrCodeID)

	// 生成 device_secret
	deviceSecret := generateSecureKey(32)

	// 建立帳號（待審批）
	db.Exec(`
		INSERT OR REPLACE INTO accounts (account_id, device_id, device_secret, permission_level, status, created_at)
		VALUES (?, ?, ?, 'L2', 'pending_approval', ?)
	`, accountID, req.DeviceID, deviceSecret, time.Now())

	log.Printf("📱 新設備註冊: account=%s, device=%s (待審批)\n", accountID, req.DeviceID)
	logAudit(accountID, "register_device", "account", r.RemoteAddr, req.DeviceID, "success", "pending_approval")

	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: "設備已註冊，等待電腦端核准",
		Data: map[string]string{
			"account_id":    accountID,
			"status":        "pending_approval",
			"device_secret": deviceSecret,
		},
	})
}

// 手機端輪詢審批狀態（公開端點，用 device_secret 授權，免認證）
// GET /auth/poll?account_id=xxx&device_secret=xxx
//
//	pending_approval → {status:"pending..."}；rejected/disabled → {status:<status>}
//	active → Set-Cookie(session_token, HttpOnly) + {status:"approved"}
func pollStatusHandler(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	deviceSecret := r.URL.Query().Get("device_secret")
	if accountID == "" || deviceSecret == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "缺少 account_id 或 device_secret"})
		return
	}

	var storedSecret, status, deviceID string
	err := db.QueryRow(
		"SELECT device_secret, status, device_id FROM accounts WHERE account_id = ?",
		accountID,
	).Scan(&storedSecret, &status, &deviceID)
	if err != nil {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}

	// 用 constant-time 比較驗證 device_secret，避免時序攻擊
	if subtle.ConstantTimeCompare([]byte(storedSecret), []byte(deviceSecret)) != 1 {
		sendJSON(w, http.StatusForbidden, APIResponse{Status: "error", Error: "device_secret 不符"})
		logAudit(accountID, "poll_status", "account", r.RemoteAddr, deviceID, "denied", "bad_device_secret")
		return
	}

	if status != "active" {
		// pending_approval / rejected / disabled：原樣回報，手機端據此顯示
		sendJSON(w, http.StatusOK, APIResponse{Status: "success", Data: map[string]string{"status": status}})
		return
	}

	// 已核准：撈最新 session_token，種成 HttpOnly cookie
	var token string
	err = db.QueryRow(
		"SELECT session_token FROM sessions WHERE account_id = ? AND device_id = ? ORDER BY created_at DESC LIMIT 1",
		accountID, deviceID,
	).Scan(&token)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "帳號已核准但找不到 session"})
		return
	}
	setSessionCookie(w, r, token)
	logAudit(accountID, "poll_status", "account", r.RemoteAddr, deviceID, "success", "approved")
	sendJSON(w, http.StatusOK, APIResponse{Status: "success", Data: map[string]string{"status": "approved"}})
}

// 種 session_token cookie：HttpOnly 防 XSS 竊取；經 HTTPS 隧道時（X-Forwarded-Proto=https）才加 Secure
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   90 * 24 * 60 * 60, // 90 天，與 JWT 有效期一致
	})
}

// --- 管理端點（電腦端調用） ---

// 生成 QR Code
func generateQRHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}

	var req struct {
		AccountID string `json:"account_id"`
		Username  string `json:"username"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.AccountID == "" {
		req.AccountID = generateID("user")
	}

	tempKey := generateSecureKey(16)
	qrCodeID := generateID("qr")
	expiresAt := time.Now().Add(1 * time.Hour)

	// 寫入資料庫
	_, err := db.Exec(`
		INSERT INTO qr_codes (qr_code_id, temp_key, account_id, generated_at, expires_at, used)
		VALUES (?, ?, ?, ?, ?, 0)
	`, qrCodeID, tempKey, req.AccountID, time.Now(), expiresAt)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}

	// QR Code 內容（URL 格式，手機掃描後直接跳轉到註冊頁面）
	qrContent := fmt.Sprintf("%s/auth/register?temp_key=%s&account_id=%s",
		publicURL, tempKey, req.AccountID)

	// 生成 QR Code 圖像
	filename := fmt.Sprintf("qrcode_%s.png", req.AccountID)
	if err := qrcode.WriteFile(qrContent, qrcode.Medium, 256, filename); err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}

	log.Printf("🔑 QR Code 生成: account=%s, 有效期=%s\n", req.AccountID, expiresAt.Format("15:04:05"))
	logAudit("admin", "generate_qr", "qr_code", r.RemoteAddr, "", "success", "")

	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: "QR Code 已生成",
		Data: map[string]string{
			"account_id":   req.AccountID,
			"temp_key":     tempKey,
			"qr_code_file": filename,
			"expires_at":   expiresAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// 查看待審批帳號
func pendingAccountsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT account_id, device_id, permission_level, status, created_at 
		FROM accounts 
		WHERE status = 'pending_approval'
		ORDER BY created_at DESC
	`)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}
	defer rows.Close()

	var accounts []map[string]string
	for rows.Next() {
		var accountID, deviceID, permission, status, createdAt string
		rows.Scan(&accountID, &deviceID, &permission, &status, &createdAt)
		accounts = append(accounts, map[string]string{
			"account_id": accountID,
			"device_id":  deviceID,
			"permission": permission,
			"status":     status,
			"created_at": createdAt,
		})
	}

	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data:   accounts,
	})
}

// 核准帳號
func approveAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}

	var req struct {
		AccountID  string `json:"account_id"`
		Permission string `json:"permission"`
		Action     string `json:"action"` // "approve" 或 "reject"
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.AccountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}

	if req.Permission == "" {
		req.Permission = "L2" // 預設 L2
	}

	if req.Action == "" {
		req.Action = "approve"
	}

	// 查詢帳號
	var deviceID string
	err := db.QueryRow("SELECT device_id FROM accounts WHERE account_id = ?", req.AccountID).Scan(&deviceID)
	if err != nil {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}

	if req.Action == "reject" {
		db.Exec("UPDATE accounts SET status = 'rejected' WHERE account_id = ?", req.AccountID)
		log.Printf("❌ 帳號已拒絕: %s\n", req.AccountID)
		logAudit("admin", "reject_account", req.AccountID, r.RemoteAddr, "", "success", "")
		sendJSON(w, http.StatusOK, APIResponse{Status: "success", Message: "帳號已拒絕"})
		return
	}

	// 核准帳號
	db.Exec("UPDATE accounts SET status = 'active', permission_level = ?, approved_at = ? WHERE account_id = ?",
		req.Permission, time.Now(), req.AccountID)

	// 生成 JWT session_token（90 天）
	sessionToken, err := generateJWT(req.AccountID, deviceID, req.Permission, 90*24*time.Hour)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "JWT 生成失敗"})
		return
	}

	// 生成 refresh_token（2 年）
	refreshToken, err := generateJWT(req.AccountID, deviceID, req.Permission, 2*365*24*time.Hour)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "Refresh Token 生成失敗"})
		return
	}

	// 存儲 Session
	sessionID := generateID("session")
	db.Exec(`
		INSERT INTO sessions (session_id, account_id, device_id, session_token, refresh_token, created_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, sessionID, req.AccountID, deviceID, sessionToken, refreshToken, time.Now(), time.Now().Add(90*24*time.Hour), time.Now())

	log.Printf("✅ 帳號已核准: %s (權限: %s)\n", req.AccountID, req.Permission)
	logAudit("admin", "approve_account", req.AccountID, r.RemoteAddr, "", "success", req.Permission)

	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: fmt.Sprintf("帳號 %s 已核准（權限: %s）", req.AccountID, req.Permission),
		Data: map[string]string{
			"account_id":    req.AccountID,
			"permission":    req.Permission,
			"session_token": sessionToken,
			"refresh_token": refreshToken,
			"expires_at":    time.Now().Add(90 * 24 * time.Hour).Format("2006-01-02 15:04:05"),
		},
	})
}

// 查看所有帳號
func listAccountsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT account_id, device_id, permission_level, status, created_at, approved_at, last_login
		FROM accounts ORDER BY created_at DESC
	`)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}
	defer rows.Close()

	var accounts []map[string]interface{}
	for rows.Next() {
		var accountID, deviceID, permission, status string
		var createdAt string
		var approvedAt, lastLogin sql.NullString
		rows.Scan(&accountID, &deviceID, &permission, &status, &createdAt, &approvedAt, &lastLogin)
		accounts = append(accounts, map[string]interface{}{
			"account_id":  accountID,
			"device_id":   deviceID,
			"permission":  permission,
			"status":      status,
			"created_at":  createdAt,
			"approved_at": approvedAt.String,
			"last_login":  lastLogin.String,
		})
	}

	sendJSON(w, http.StatusOK, APIResponse{Status: "success", Data: accounts})
}

// 驗證 Token（手機端可用此端點確認 token 是否有效）
func verifyTokenHandler(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	deviceID := r.Header.Get("X-Device-ID")
	permission := r.Header.Get("X-Permission")

	// 取得 token 過期時間
	tokenString := extractToken(r)
	claims, err := validateJWT(tokenString)
	if err != nil {
		sendJSON(w, http.StatusUnauthorized, APIResponse{Status: "error", Error: "Token 無效"})
		return
	}

	sendJSON(w, http.StatusOK, APIResponse{
		Status: "valid",
		Data: map[string]string{
			"account_id": accountID,
			"device_id":  deviceID,
			"permission": permission,
			"expires_at": claims.ExpiresAt.Time.Format("2006-01-02 15:04:05"),
		},
	})
}

// 查看審計日誌
func viewLogsHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "50"
	}
	// 限制為純數字，防止 SQL Injection
	limitVal := 50
	fmt.Sscanf(limitStr, "%d", &limitVal)
	if limitVal <= 0 || limitVal > 1000 {
		limitVal = 50
	}

	rows, err := db.Query(`
		SELECT log_id, account_id, operation, resource, ip_address, device_id, timestamp, status, reason
		FROM audit_logs ORDER BY timestamp DESC LIMIT ?`, limitVal)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}
	defer rows.Close()

	var logs []map[string]string
	for rows.Next() {
		var logID, accountID, operation, resource, status string
		var ipAddr, deviceID, timestamp, reason sql.NullString
		rows.Scan(&logID, &accountID, &operation, &resource, &ipAddr, &deviceID, &timestamp, &status, &reason)
		logs = append(logs, map[string]string{
			"log_id":     logID,
			"account_id": accountID,
			"operation":  operation,
			"resource":   resource,
			"ip_address": ipAddr.String,
			"device_id":  deviceID.String,
			"timestamp":  timestamp.String,
			"status":     status,
			"reason":     reason.String,
		})
	}

	sendJSON(w, http.StatusOK, APIResponse{Status: "success", Data: logs})
}

// GET /admin/account-secrets?account_id=xxx
// 供控制面板「E2E 測試」按鈕使用：回傳指定帳號的最新 session_token 與 device_secret。
// ⚠️  此端點僅供 localhost 管理員使用，生產環境應額外加 IP 白名單保護。
func accountSecretsHandler(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}

	// 確認帳號存在且為 active 狀態
	var status, deviceSecret string
	err := db.QueryRow(
		"SELECT status, device_secret FROM accounts WHERE account_id = ?", accountID,
	).Scan(&status, &deviceSecret)
	if err != nil {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}
	if status != "active" {
		sendJSON(w, http.StatusForbidden, APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("帳號狀態為 %s，非 active，無法取得測試憑證", status),
		})
		return
	}

	// 取得最新一筆 session_token
	var sessionToken string
	err = db.QueryRow(
		"SELECT session_token FROM sessions WHERE account_id = ? ORDER BY created_at DESC LIMIT 1",
		accountID,
	).Scan(&sessionToken)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "找不到此帳號的 Session，請重新核准"})
		return
	}

	logAudit("admin", "fetch_account_secrets", accountID, r.RemoteAddr, "", "success", "e2e_test_open")
	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data: map[string]string{
			"account_id":    accountID,
			"session_token": sessionToken,
			"device_secret": deviceSecret,
		},
	})
}

// --- 需認證的端點 ---

// 聊天（需要 L2+ 權限）
// 同時支援明文請求與 E2E 加密請求（根據是否含 encrypted_key 欄位自動判斷）
func chatHandler(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	deviceID := r.Header.Get("X-Device-ID")

	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "讀取請求體失敗"})
		return
	}

	// 解析最外層 JSON，判斷是 E2E 加密請求還是明文請求
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "無效的 JSON 格式"})
		return
	}

	var message string
	if _, isE2E := rawMap["encrypted_key"]; isE2E {
		// ── E2E 加密請求 ─────────────────────────────────────────────
		var e2eReq E2ERequest
		if err := json.Unmarshal(bodyBytes, &e2eReq); err != nil {
			sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "E2E 請求格式無效"})
			return
		}
		if e2eReq.EncryptedKey == "" || e2eReq.Ciphertext == "" || e2eReq.Nonce == "" || e2eReq.HMACSignature == "" {
			sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "E2E 請求缺少必要欄位（encrypted_key/ciphertext/nonce/hmac_signature）"})
			return
		}

		// 從資料庫取得 device_secret 用於 HMAC 驗證
		var deviceSecret string
		if dbErr := db.QueryRow("SELECT device_secret FROM accounts WHERE account_id = ?", accountID).Scan(&deviceSecret); dbErr != nil {
			sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "無法取得 device_secret"})
			logAudit(accountID, "chat_e2e", "llama", r.RemoteAddr, deviceID, "denied", "device_secret_query_failed")
			return
		}

		// E2E 解密：HMAC 驗證 → RSA-OAEP 解密 → AES-256-GCM 解密
		plainBytes, decErr := decryptE2ERequest(e2eReq, deviceSecret)
		if decErr != nil {
			sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "E2E 解密失敗: " + decErr.Error()})
			logAudit(accountID, "chat_e2e", "llama", r.RemoteAddr, deviceID, "denied", decErr.Error())
			return
		}

		// 解析解密後的明文 JSON 取得 message
		var inner struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(plainBytes, &inner); err != nil || inner.Message == "" {
			sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "解密後 JSON 格式無效或 message 為空"})
			return
		}
		message = inner.Message
		log.Printf("🔐 E2E 解密成功: account=%s\n", accountID)
		logAudit(accountID, "chat_e2e", "llama", r.RemoteAddr, deviceID, "success", "e2e_decrypted")
	} else {
		// ── 明文請求（向後相容舊版客戶端）──────────────────────────
		var plainReq struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(bodyBytes, &plainReq); err != nil || plainReq.Message == "" {
			sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "message 為必填"})
			return
		}
		message = plainReq.Message
		log.Printf("💬 明文聊天請求: account=%s\n", accountID)
		logAudit(accountID, "chat", "llama", r.RemoteAddr, deviceID, "success", "plaintext")
	}

	// TODO: 轉發到 llama.cpp API 並記錄 token 用量
	// 目前先回傳模擬回應
	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data: map[string]interface{}{
			"response":    "[代理層] 收到你的訊息: " + message,
			"account_id":  accountID,
			"tokens_used": 0,
			"note":        "尚未連接 llama.cpp API，這是測試回應",
		},
	})
}

// 查看自己的對話
func myConversationsHandler(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")

	rows, err := db.Query(`
		SELECT conv_id, user_message, ai_response, total_tokens, created_at
		FROM conversations
		WHERE account_id = ?
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}
	defer rows.Close()

	var convs []map[string]interface{}
	for rows.Next() {
		var convID, userMsg, aiResp, createdAt string
		var totalTokens int
		rows.Scan(&convID, &userMsg, &aiResp, &totalTokens, &createdAt)
		convs = append(convs, map[string]interface{}{
			"conv_id":      convID,
			"user_message": userMsg,
			"ai_response":  aiResp,
			"total_tokens": totalTokens,
			"created_at":   createdAt,
		})
	}

	if convs == nil {
		sendJSON(w, http.StatusOK, APIResponse{
			Status:  "success",
			Message: "無資料",
			Data:    []interface{}{},
		})
		return
	}

	sendJSON(w, http.StatusOK, APIResponse{Status: "success", Data: convs})
}

// 代理到 llama.cpp（需認證，用於 Web UI）
func proxyToLlamaAuthenticated(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	log.Printf("🔄 代理轉發: account=%s, path=%s\n", accountID, r.URL.Path)

	// 移除內部 Header，不暴露給 llama.cpp
	r.Header.Del("X-Account-ID")
	r.Header.Del("X-Device-ID")
	r.Header.Del("X-Permission")
	r.Header.Del("Authorization")

	llamaURL, _ := url.Parse("http://127.0.0.1:8080")
	proxy := httputil.NewSingleHostReverseProxy(llamaURL)
	proxy.ServeHTTP(w, r)
}

// 代理到 llama.cpp（無認證，用於公開的 Web UI 靜態資源）
func proxyToLlamaPublic(w http.ResponseWriter, r *http.Request) {
	llamaURL, _ := url.Parse("http://127.0.0.1:8080")
	proxy := httputil.NewSingleHostReverseProxy(llamaURL)
	proxy.ServeHTTP(w, r)
}

// ============================================================
// 中間件
// ============================================================

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		log.Printf("[%s] %s %s %v\n", r.Method, r.URL.Path, r.RemoteAddr, duration)
	})
}

// ============================================================
// 主程序
// ============================================================

func main() {
	// 建立日誌
	logFile, err := os.OpenFile("proxy.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("無法建立日誌檔案:", err)
		return
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("==================================================")
	log.Println("llama.cpp 代理層啟動（階段 3：強制認證版）")
	log.Println("==================================================")

	// 從環境變量讀取公網 URL（未設定則使用 localhost 作為預設）
	// TrimSpace 清理尾端空白，避免複製 Cloudflare URL 時帶入空格/換行，導致 QR URL 出現 "...com /auth/register"
	publicURL = strings.TrimSpace(os.Getenv("LLAMA_PUBLIC_URL"))
	if publicURL == "" {
		publicURL = "http://127.0.0.1:8081"
		log.Println("⚠️  LLAMA_PUBLIC_URL 未設定，QR Code 將使用 localhost URL")
		log.Println("   請設定: $env:LLAMA_PUBLIC_URL = \"https://your-tunnel.trycloudflare.com\"")
	} else {
		log.Printf("✓ 公網 URL: %s\n", publicURL)
	}

	// 生成伺服器密鑰（生產環境應從文件/環境變量讀取）
	serverSecret = []byte(generateSecureKey(32))
	log.Println("✓ 伺服器密鑰已生成")

	// 初始化資料庫
	if err := initDB(); err != nil {
		log.Fatalf("資料庫初始化失敗: %v\n", err)
	}
	defer db.Close()

	// 初始化 E2E RSA-2048 密鑰對（自動載入已有密鑰或生成新密鑰）
	if err := loadOrGenerateRSAKeyPair(); err != nil {
		log.Fatalf("E2E 密鑰初始化失敗: %v\n", err)
	}

	// ==================
	// 路由設定
	// ==================

	// 公開端點（無需認證）
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/auth/register", registerDeviceHandler)
	http.HandleFunc("/auth/poll", pollStatusHandler)
	http.HandleFunc("/api/public-key", publicKeyHandler)                        // RSA E2E 公鑰（前端加密用）
	http.HandleFunc("/e2e-test", func(w http.ResponseWriter, r *http.Request) { // E2E 加密測試頁（開發用）
		http.ServeFile(w, r, "e2e_test.html")
	})

	// 管理端點（電腦端調用，生產環境應限制 IP）
	http.HandleFunc("/admin/generate-qr", generateQRHandler)
	http.HandleFunc("/admin/pending", pendingAccountsHandler)
	http.HandleFunc("/admin/approve", approveAccountHandler)
	http.HandleFunc("/admin/accounts", listAccountsHandler)
	http.HandleFunc("/admin/logs", viewLogsHandler)
	http.HandleFunc("/admin/account-secrets", accountSecretsHandler) // 供控制面板取得 token+secret 開啟 E2E 測試頁

	// 需認證的端點
	http.HandleFunc("/auth/verify", authMiddleware(verifyTokenHandler, "L1"))
	http.HandleFunc("/api/chat", authMiddleware(chatHandler, "L2"))
	http.HandleFunc("/api/conversations", authMiddleware(myConversationsHandler, "L1"))
	http.HandleFunc("/api/llama/", authMiddleware(proxyToLlamaAuthenticated, "L2"))

	// 所有其他請求（強制認證後轉發到 llama.cpp Web UI）
	http.HandleFunc("/", authMiddleware(proxyToLlamaAuthenticated, "L1"))

	// 啟動
	port := ":8081"
	log.Println("")
	log.Printf("🚀 代理層監聽在 %s\n", port)
	log.Println("")
	log.Println("📌 公開端點:")
	log.Printf("   健康檢查:     GET  http://127.0.0.1%s/health\n", port)
	log.Printf("   設備註冊:     POST http://127.0.0.1%s/auth/register\n", port)
	log.Println("")
	log.Println("🔧 管理端點（電腦端）:")
	log.Printf("   生成 QR Code: POST http://127.0.0.1%s/admin/generate-qr\n", port)
	log.Printf("   待審批帳號:   GET  http://127.0.0.1%s/admin/pending\n", port)
	log.Printf("   核准帳號:     POST http://127.0.0.1%s/admin/approve\n", port)
	log.Printf("   所有帳號:     GET  http://127.0.0.1%s/admin/accounts\n", port)
	log.Printf("   審計日誌:     GET  http://127.0.0.1%s/admin/logs\n", port)
	log.Printf("   帳號憑證:     GET  http://127.0.0.1%s/admin/account-secrets?account_id=xxx\n", port)
	log.Println("")
	log.Println("🔒 需認證端點:")
	log.Printf("   聊天:         POST http://127.0.0.1%s/api/chat\n", port)
	log.Printf("   我的對話:     GET  http://127.0.0.1%s/api/conversations\n", port)
	log.Println("")
	log.Println("🔒 需認證端點（Bearer Token 必填）:")
	log.Printf("   驗證 Token:     GET  http://127.0.0.1%s/auth/verify\n", port)
	log.Println("")
	log.Println("🌐 llama.cpp Web UI（需認證）:")
	log.Printf("   http://127.0.0.1%s/  ← 需帶 Authorization: Bearer <token>\n", port)
	log.Println("")

	if err := http.ListenAndServe(port, loggingMiddleware(http.DefaultServeMux)); err != nil {
		log.Fatalf("啟動失敗: %v\n", err)
	}
}
