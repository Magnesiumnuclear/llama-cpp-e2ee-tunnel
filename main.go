package main

import (
	"bytes"
	"compress/gzip"
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
		// 區分 qr_codes 的用途：'register'（掃碼註冊）vs 'relogin'（重新登入）。
		// 舊資料列預設為 'register'，避免 relogin code 被拿去 /auth/register 重放。
		"ALTER TABLE qr_codes ADD COLUMN kind TEXT DEFAULT 'register'",
		// JWT 撤銷用：簽發時間早於此 Unix 秒數的 token 一律視為已撤銷（0＝未撤銷）。
		"ALTER TABLE accounts ADD COLUMN tokens_valid_after INTEGER DEFAULT 0",
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

	// 已移除 ?token= URL 參數回退：避免長效 90 天 JWT 出現在網址
	// （會進 Cloudflare edge log / 瀏覽器歷史 / Referer）。認證只接受 Header 或 Cookie。
	return ""
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

		// 檢查帳號狀態 + JWT 撤銷狀態
		var status string
		var tokensValidAfter int64
		err = db.QueryRow(
			"SELECT status, tokens_valid_after FROM accounts WHERE account_id = ?", claims.AccountID,
		).Scan(&status, &tokensValidAfter)
		if err != nil || status != "active" {
			sendJSON(w, http.StatusForbidden, APIResponse{
				Status: "error",
				Error:  "帳號未啟用或已被停用",
			})
			logAudit(claims.AccountID, "auth_check", r.URL.Path, r.RemoteAddr, claims.DeviceID, "denied", "account_inactive")
			return
		}

		// 撤銷檢查：帳號設了 tokens_valid_after，且此 token 簽發時間早於它 → 已被撤銷。
		// 一次 UPDATE 即可讓該帳號所有舊 token 失效（處理憑證外洩）；使用者重新登入取得 iat 較新的 token 即可恢復。
		if tokensValidAfter > 0 && claims.IssuedAt != nil && claims.IssuedAt.Time.Unix() < tokensValidAfter {
			sendJSON(w, http.StatusUnauthorized, APIResponse{
				Status: "error",
				Error:  "Token 已被撤銷，請重新登入",
			})
			logAudit(claims.AccountID, "auth_check", r.URL.Path, r.RemoteAddr, claims.DeviceID, "denied", "token_revoked")
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
		"SELECT qr_code_id, account_id, expires_at, used FROM qr_codes WHERE temp_key = ? AND kind = 'register'",
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

// adminGate 保護所有 /admin/* 端點：要求帶正確的 X-Admin-Token。
//
// 為何不用 RemoteAddr/loopback 判斷：cloudflared 以 `--url http://localhost:8081` 連入，
// 所有 tunnel 流量的 RemoteAddr 也是 loopback，無法藉此區分「本機控制面板」與「公網攻擊者」。
// 改用「只有控制面板知道的一次性隨機 token」（啟動時以 LLAMA_ADMIN_TOKEN env 注入）做授權，
// 此 token 不會出現在任何 URL 或對外回應中。未設定 token 時一律拒絕（僅面板會用到 /admin/*）。
func adminGate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(os.Getenv("LLAMA_ADMIN_TOKEN"))
		got := r.Header.Get("X-Admin-Token")
		if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			io.Copy(io.Discard, r.Body) // 先排空請求 body，避免提前回應導致 POST 連線被重置
			sendJSON(w, http.StatusForbidden, APIResponse{Status: "error", Error: "管理端點僅限本機控制面板"})
			logAudit("unknown", "admin_gate", r.URL.Path, r.RemoteAddr, "", "denied", "bad_admin_token")
			return
		}
		next(w, r)
	}
}

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

// 刪除帳號（管理端點）
func deleteAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}
	// 確認帳號存在
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE account_id = ?", req.AccountID).Scan(&exists)
	if err != nil || exists == 0 {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}
	// 刪除相關 sessions
	db.Exec("DELETE FROM sessions WHERE account_id = ?", req.AccountID)
	// 刪除帳號
	db.Exec("DELETE FROM accounts WHERE account_id = ?", req.AccountID)
	log.Printf("🗑 帳號已刪除: %s\n", req.AccountID)
	logAudit("admin", "delete_account", req.AccountID, r.RemoteAddr, "", "success", "")
	sendJSON(w, http.StatusOK, APIResponse{Status: "success", Message: fmt.Sprintf("帳號 %s 已刪除", req.AccountID)})
}

// POST /admin/revoke-sessions — 撤銷指定帳號目前所有 token（憑證外洩時用）。
// 將 tokens_valid_after 設為現在：所有簽發時間更早的 token 立即失效；使用者重新登入即可恢復。
func revokeSessionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}
	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE account_id = ?", req.AccountID).Scan(&exists); err != nil || exists == 0 {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}
	db.Exec("UPDATE accounts SET tokens_valid_after = ? WHERE account_id = ?", time.Now().Unix(), req.AccountID)
	db.Exec("DELETE FROM sessions WHERE account_id = ?", req.AccountID)
	logAudit("admin", "revoke_sessions", req.AccountID, r.RemoteAddr, "", "success", "")
	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: fmt.Sprintf("已撤銷帳號 %s 的所有登入，使用者需重新登入", req.AccountID),
	})
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

// --- 重新登入（解決 tunnel 換網址 / 關閉網頁後無法登入）---
//
// 設計重點：長效 90 天 JWT 永不進入 URL；URL 只含一次性、5 分鐘 TTL 的 relogin code。
// 流程：面板（adminGate 保護）鑄造 code → 使用者在「目前公網 host」開 /auth/relogin?code=...
// → GET 顯示同源確認頁（不消耗 code）→ 同源 POST 才原子性消耗 code、重簽 JWT、種 cookie、轉址 /。
// account_id 全程不變，故對話歷史天然保留（為未來「對話保留」鋪路）。

// POST /admin/relogin-code — 為既有 active 帳號鑄造一次性重新登入連結（含伺服器端 QR）。
func reloginCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}
	// 拒絕在非 HTTPS 公網 URL 下產生帶憑證的連結（cookie 會綁到 localhost／非 Secure）。
	if !strings.HasPrefix(publicURL, "https://") {
		sendJSON(w, http.StatusConflict, APIResponse{Status: "error", Error: "公網 HTTPS URL 尚未就緒，無法產生重新登入連結"})
		return
	}
	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE account_id = ?", req.AccountID).Scan(&status); err != nil {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}
	if status != "active" {
		sendJSON(w, http.StatusConflict, APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("帳號狀態為 %s，非 active，無法重新登入", status),
		})
		return
	}

	code, expiresAt, err := insertOneTimeCode(req.AccountID, "relogin", 5*time.Minute)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}

	// 連結綁定「目前公網 host」（與面板 self._public_url 同源）。
	reloginURL := fmt.Sprintf("%s/auth/relogin?code=%s", publicURL, code)
	filename := fmt.Sprintf("relogin_%s.png", req.AccountID)
	if err := qrcode.WriteFile(reloginURL, qrcode.Medium, 256, filename); err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}

	logAudit("admin", "mint_relogin", req.AccountID, r.RemoteAddr, "", "success", "")
	sendJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: "重新登入連結已產生",
		Data: map[string]string{
			"account_id":   req.AccountID,
			"relogin_url":  reloginURL,
			"qr_code_file": filename,
			"expires_at":   expiresAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// GET/POST /auth/relogin — 公開端點（自我授權於一次性 code）。
// GET：僅驗證 code 並顯示同源確認頁，不消耗。POST：同源檢查 → 原子消耗 → 重簽 JWT → 種 cookie → 轉址 /。
func reloginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store, private")

	switch r.Method {
	case http.MethodGet:
		code := r.URL.Query().Get("code")
		accountID, ok := lookupOneTimeCode(code, "relogin")
		if !ok {
			reloginErrorPage(w, "連結無效、已使用或已過期，請回電腦端重新產生。")
			return
		}
		reloginConfirmPage(w, code, accountID)

	case http.MethodPost:
		// CSRF 防護：確認表單帶有以 serverSecret 對 code 簽章的 token。跨站頁面因 CORS 無法讀取
		// 我們的確認頁內容，也就拿不到此 token、無法偽造登入 POST。比 Origin/Referer 標頭可靠
		// （不受瀏覽器是否送出標頭、以及 Referrer-Policy: no-referrer 影響）。
		r.ParseForm()
		code := r.FormValue("code")
		csrf := r.FormValue("csrf")
		if code == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(reloginCSRF(code))) != 1 {
			reloginErrorPage(w, "請求來源無效，請回電腦端重新產生連結。")
			logAudit("unknown", "relogin", "account", r.RemoteAddr, "", "denied", "bad_csrf")
			return
		}

		// 原子性消耗：唯一一筆 used=0 且未過期的 relogin code 才會被消耗。
		accountID, ok := consumeOneTimeCode(code, "relogin")
		if !ok {
			reloginErrorPage(w, "連結無效、已使用或已過期，請回電腦端重新產生。")
			return
		}

		// 消耗後再次確認帳號 active，並從 accounts 取 device_id/permission（不信任 client）。
		var deviceID, permission, status string
		err := db.QueryRow(
			"SELECT device_id, permission_level, status FROM accounts WHERE account_id = ?", accountID,
		).Scan(&deviceID, &permission, &status)
		if err != nil || status != "active" {
			reloginErrorPage(w, "帳號未啟用或不存在，無法登入。")
			logAudit(accountID, "relogin", "account", r.RemoteAddr, deviceID, "denied", "account_inactive")
			return
		}
		if permission == "" {
			permission = "L2"
		}

		// serverSecret 每次重啟會更換 → 一律重簽新 JWT，確保 cookie 對目前 secret 有效。
		sessionToken, err := generateJWT(accountID, deviceID, permission, 90*24*time.Hour)
		if err != nil {
			reloginErrorPage(w, "登入失敗（JWT 生成錯誤）。")
			return
		}
		refreshToken, _ := generateJWT(accountID, deviceID, permission, 2*365*24*time.Hour)

		// 清掉同 account+device 的舊 session 列再插入新的，避免重複登入造成列數無限增長。
		db.Exec("DELETE FROM sessions WHERE account_id = ? AND device_id = ?", accountID, deviceID)
		sessionID := generateID("session")
		db.Exec(`
			INSERT INTO sessions (session_id, account_id, device_id, session_token, refresh_token, created_at, expires_at, last_activity)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, sessionID, accountID, deviceID, sessionToken, refreshToken, time.Now(), time.Now().Add(90*24*time.Hour), time.Now())

		// 寫入目前 schema 有欄位卻從未被更新的 last_login（供總覽顯示／未來保留策略）。
		db.Exec("UPDATE accounts SET last_login = ? WHERE account_id = ?", time.Now(), accountID)

		setSessionCookie(w, r, sessionToken)
		logAudit(accountID, "relogin", "account", r.RemoteAddr, deviceID, "success", "")
		http.Redirect(w, r, "/", http.StatusSeeOther)

	default:
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 GET/POST"})
	}
}

// --- 一次性 code 共用工具（relogin / e2e 等流程共用，以 kind 區分）---

// insertOneTimeCode 鑄造一枚一次性 code 存入 qr_codes，回傳 (code, 到期時間)。
func insertOneTimeCode(accountID, kind string, ttl time.Duration) (string, time.Time, error) {
	code := generateSecureKey(16)
	expiresAt := time.Now().Add(ttl)
	_, err := db.Exec(`
		INSERT INTO qr_codes (qr_code_id, temp_key, account_id, generated_at, expires_at, used, kind)
		VALUES (?, ?, ?, ?, ?, 0, ?)
	`, generateID(kind), code, accountID, time.Now(), expiresAt, kind)
	return code, expiresAt, err
}

// lookupOneTimeCode 僅驗證（不消耗）一枚指定 kind 的有效 code，回傳其 account_id。
func lookupOneTimeCode(code, kind string) (string, bool) {
	if code == "" {
		return "", false
	}
	var accountID string
	err := db.QueryRow(
		"SELECT account_id FROM qr_codes WHERE temp_key = ? AND kind = ? AND used = 0 AND expires_at > ?",
		code, kind, time.Now(),
	).Scan(&accountID)
	if err != nil {
		return "", false
	}
	return accountID, true
}

// consumeOneTimeCode 原子性消耗一枚指定 kind 的有效 code（單次使用），回傳其 account_id。
func consumeOneTimeCode(code, kind string) (string, bool) {
	if code == "" {
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	res, _ := db.Exec(
		"UPDATE qr_codes SET used = 1, used_at = ? WHERE temp_key = ? AND used = 0 AND kind = ? AND expires_at > ?",
		time.Now(), code, kind, time.Now())
	var affected int64
	if res != nil {
		affected, _ = res.RowsAffected()
	}
	if affected != 1 {
		return "", false
	}
	var accountID string
	db.QueryRow("SELECT account_id FROM qr_codes WHERE temp_key = ?", code).Scan(&accountID)
	return accountID, accountID != ""
}

// reloginCSRF 以 serverSecret 對 code 簽章，作為確認表單的 CSRF token（同步器 token 模式）。
// 跨站頁面因 CORS 無法讀取我們確認頁的內容，故拿不到此 token，也就無法偽造登入 POST；
// 不依賴 Origin/Referer 標頭，避免瀏覽器差異與 Referrer-Policy: no-referrer 造成的誤判。
func reloginCSRF(code string) string {
	mac := hmac.New(sha256.New, serverSecret)
	mac.Write([]byte("relogin:" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func reloginConfirmPage(w http.ResponseWriter, code, accountID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="zh-Hant"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>重新登入</title><style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(160deg,#0d1024,#1b2148);font-family:system-ui,"Microsoft JhengHei",sans-serif;color:#eef1ff}
.card{background:rgba(255,255,255,.06);border:1px solid rgba(255,255,255,.12);border-radius:16px;
padding:32px 28px;max-width:340px;text-align:center}
h1{font-size:18px;margin:0 0 8px}p{color:#9aa3c8;font-size:14px;margin:0 0 22px;line-height:1.5}
b{color:#eef1ff}button{width:100%%;padding:12px;border:0;border-radius:10px;color:#fff;font-size:15px;
font-weight:700;cursor:pointer;background:linear-gradient(90deg,#6a8dff,#9b6bff)}
</style></head><body><div class="card">
<h1>重新登入</h1>
<p>以帳號 <b>%s</b> 登入並還原你的對話？</p>
<form method="post" action="/auth/relogin">
<input type="hidden" name="code" value="%s">
<input type="hidden" name="csrf" value="%s">
<button type="submit">確認登入</button>
</form></div></body></html>`, htmlEscape(accountID), htmlEscape(code), reloginCSRF(code))
}

func reloginErrorPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="zh-Hant"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>重新登入</title><style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(160deg,#0d1024,#1b2148);font-family:system-ui,"Microsoft JhengHei",sans-serif;color:#eef1ff}
.card{background:rgba(255,255,255,.06);border:1px solid rgba(255,255,255,.12);border-radius:16px;
padding:32px 28px;max-width:340px;text-align:center}
p{color:#9aa3c8;font-size:14px;margin:0;line-height:1.5}
</style></head><body><div class="card"><p>%s</p></div></body></html>`, htmlEscape(msg))
}

// htmlEscape 對插入 HTML 的字串做最小轉義，避免 account_id 等內容造成注入。
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// --- E2E 測試頁憑證交換（避免把長效 token/secret 放進 URL）---
//
// 舊作法把 session_token + device_secret 直接塞進 /e2e-test?token=&secret=（會外洩到 log/歷史/Referer）。
// 改為：面板（adminGate）鑄造一次性 e2e code → 測試頁開 /e2e-test?code=...（URL 只含短效一次性 code）
// → 頁面 POST /e2e-test/exchange { code } 於「回應 body」換回憑證（不入 URL）。

// POST /admin/e2e-code — 為 active 帳號鑄造一次性 e2e code（供測試頁換取憑證）。
func e2eCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "account_id 為必填"})
		return
	}
	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE account_id = ?", req.AccountID).Scan(&status); err != nil {
		sendJSON(w, http.StatusNotFound, APIResponse{Status: "error", Error: "找不到此帳號"})
		return
	}
	if status != "active" {
		sendJSON(w, http.StatusConflict, APIResponse{Status: "error", Error: fmt.Sprintf("帳號狀態為 %s，非 active", status)})
		return
	}
	code, expiresAt, err := insertOneTimeCode(req.AccountID, "e2e", 5*time.Minute)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: err.Error()})
		return
	}
	logAudit("admin", "mint_e2e_code", req.AccountID, r.RemoteAddr, "", "success", "")
	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data: map[string]string{
			"account_id": req.AccountID,
			"code":       code,
			"expires_at": expiresAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// POST /e2e-test/exchange — 公開端點。以一次性 e2e code 換取 session_token + device_secret（於回應 body）。
func e2eExchangeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	accountID, ok := consumeOneTimeCode(req.Code, "e2e")
	if !ok {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "code 無效、已使用或已過期"})
		return
	}
	var deviceID, permission, status, deviceSecret string
	err := db.QueryRow(
		"SELECT device_id, permission_level, status, device_secret FROM accounts WHERE account_id = ?", accountID,
	).Scan(&deviceID, &permission, &status, &deviceSecret)
	if err != nil || status != "active" {
		sendJSON(w, http.StatusForbidden, APIResponse{Status: "error", Error: "帳號未啟用或不存在"})
		return
	}
	if permission == "" {
		permission = "L2"
	}
	// serverSecret 每次重啟更換 → 重簽新 JWT，確保對目前 secret 有效。
	token, err := generateJWT(accountID, deviceID, permission, 90*24*time.Hour)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "JWT 生成失敗"})
		return
	}
	logAudit(accountID, "e2e_exchange", "account", r.RemoteAddr, deviceID, "success", "")
	sendJSON(w, http.StatusOK, APIResponse{
		Status: "success",
		Data: map[string]string{
			"account_id":    accountID,
			"session_token": token,
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

// e2eChatEnvelope：forked UI 送來的 E2E 聊天請求信封。
// 無 HMAC —— 認證靠 HttpOnly cookie（authMiddleware 先驗），完整性靠 AES-GCM tag。
// 解密後的明文為 OpenAI /v1/chat/completions 的請求 JSON。
type e2eChatEnvelope struct {
	EncryptedKey string `json:"encrypted_key"` // base64: RSA-OAEP(伺服器公鑰, K)
	IV           string `json:"iv"`            // base64: 12-byte AES-GCM nonce
	Ciphertext   string `json:"ciphertext"`    // base64: AES-256-GCM(K, iv, 明文) 末尾含 16-byte tag
}

// decryptE2EChat 解密聊天信封，回傳明文與本次的一次性 AES-256 金鑰 K（供回應串流加密重用）。
func decryptE2EChat(env e2eChatEnvelope) (plaintext, key []byte, err error) {
	encKey, err := base64.StdEncoding.DecodeString(env.EncryptedKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypted_key base64: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil {
		return nil, nil, fmt.Errorf("iv base64: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, nil, fmt.Errorf("ciphertext base64: %w", err)
	}
	if serverE2EPrivKey == nil {
		return nil, nil, fmt.Errorf("E2E 私鑰尚未初始化")
	}
	key, err = rsa.DecryptOAEP(sha256.New(), rand.Reader, serverE2EPrivKey, encKey, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("RSA-OAEP 解密 K 失敗: %w", err)
	}
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("K 長度無效（應 32 bytes，實際 %d）", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("AES-GCM: %w", err)
	}
	if len(iv) != gcm.NonceSize() {
		return nil, nil, fmt.Errorf("iv 長度無效（應 %d，實際 %d）", gcm.NonceSize(), len(iv))
	}
	plaintext, err = gcm.Open(nil, iv, ct, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("AES-256-GCM 解密失敗: %w", err)
	}
	return plaintext, key, nil
}

// encE2EFrame 用 K（gcm）對一塊 bytes 做 AES-256-GCM 加密，回傳 "base64(iv).base64(ct+tag)"。
func encE2EFrame(gcm cipher.AEAD, chunk []byte) (string, error) {
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, iv, chunk, nil)
	return base64.StdEncoding.EncodeToString(iv) + "." + base64.StdEncoding.EncodeToString(ct), nil
}

// ============================================================
// /api/e2e/chat 濫用防禦：併發上限 + 速率限制 + 重放快取
// 皆以記憶體儲存（單機單實例足夠；proxy 重啟即清空）。
// ============================================================

const (
	e2eMaxConcurrentPerAccount = 1                // 每帳號同時最多幾個推論（保護 GPU）
	e2eRateLimitMax            = 15               // 速率限制：每視窗最多請求數
	e2eRateLimitWindow         = 60 * time.Second // 速率限制視窗
	e2eReplayTTL               = 10 * time.Minute // iv 重放快取保留時間
)

var (
	e2eMu       sync.Mutex
	e2eInFlight = make(map[string]int)         // account_id → 進行中推論數
	e2eReqTimes = make(map[string][]time.Time) // account_id → 視窗內請求時間
	e2eSeenIV   = make(map[string]time.Time)   // iv(base64) → 首次見到時間
)

// e2eRateLimited 滑動視窗速率限制；未超量時記錄本次請求。回 true 表示超量。
func e2eRateLimited(accountID string) bool {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-e2eRateLimitWindow)
	var kept []time.Time
	for _, t := range e2eReqTimes[accountID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= e2eRateLimitMax {
		e2eReqTimes[accountID] = kept
		return true
	}
	e2eReqTimes[accountID] = append(kept, now)
	return false
}

// e2eCheckReplay 原子檢查並記錄 iv。回 true 表示近期已見過（重放）。
func e2eCheckReplay(iv string) bool {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	if t, ok := e2eSeenIV[iv]; ok && time.Since(t) < e2eReplayTTL {
		return true
	}
	e2eSeenIV[iv] = time.Now()
	return false
}

// e2eAcquireSlot 嘗試取得帳號的推論名額。回 false 表示已達併發上限。
func e2eAcquireSlot(accountID string) bool {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	if e2eInFlight[accountID] >= e2eMaxConcurrentPerAccount {
		return false
	}
	e2eInFlight[accountID]++
	return true
}

// e2eReleaseSlot 釋放帳號的推論名額。
func e2eReleaseSlot(accountID string) {
	e2eMu.Lock()
	defer e2eMu.Unlock()
	if e2eInFlight[accountID] > 0 {
		e2eInFlight[accountID]--
		if e2eInFlight[accountID] == 0 {
			delete(e2eInFlight, accountID)
		}
	}
}

// e2eCleanupLoop 定期清理過期的重放 iv 與速率視窗紀錄，避免記憶體無限成長。
func e2eCleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		e2eMu.Lock()
		now := time.Now()
		for iv, t := range e2eSeenIV {
			if now.Sub(t) >= e2eReplayTTL {
				delete(e2eSeenIV, iv)
			}
		}
		cutoff := now.Add(-e2eRateLimitWindow)
		for acc, times := range e2eReqTimes {
			var kept []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					kept = append(kept, t)
				}
			}
			if len(kept) == 0 {
				delete(e2eReqTimes, acc)
			} else {
				e2eReqTimes[acc] = kept
			}
		}
		e2eMu.Unlock()
	}
}

// e2eChatHandler：forked UI 的 E2E 聊天端點（P3）。
// 解密請求信封 → 轉發 llama.cpp /v1/chat/completions（串流）→ 用同一把 K 逐塊 AES-GCM 加密成
// SSE 幀「data: b64(iv).b64(ct)」串回前端。錯誤（解密失敗 / 上游非 200）以明文回傳（決策③）。
func e2eChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, http.StatusMethodNotAllowed, APIResponse{Status: "error", Error: "只接受 POST"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "讀取請求體失敗"})
		return
	}

	var env e2eChatEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.EncryptedKey == "" || env.Ciphertext == "" || env.IV == "" {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "無效的 E2E 信封"})
		return
	}

	accountID := r.Header.Get("X-Account-ID")
	deviceID := r.Header.Get("X-Device-ID")

	// 防禦層 1：速率限制（每帳號滑動視窗）→ 429
	if e2eRateLimited(accountID) {
		sendJSON(w, http.StatusTooManyRequests, APIResponse{Status: "error", Error: "請求過於頻繁，請稍候再試"})
		logAudit(accountID, "e2e_chat", "llama", r.RemoteAddr, deviceID, "denied", "rate_limited")
		return
	}

	// 防禦層 2：重放檢查（iv 近期重複 = 重放，便宜先擋於解密前）→ 409
	if e2eCheckReplay(env.IV) {
		sendJSON(w, http.StatusConflict, APIResponse{Status: "error", Error: "重複的請求（疑似重放）已被拒絕"})
		logAudit(accountID, "e2e_chat", "llama", r.RemoteAddr, deviceID, "denied", "replay_iv")
		return
	}

	// E2E 解密：RSA 取出 K → AES-GCM 解密得明文 OpenAI 請求
	plaintext, key, decErr := decryptE2EChat(env)
	if decErr != nil {
		sendJSON(w, http.StatusBadRequest, APIResponse{Status: "error", Error: "E2E 解密失敗: " + decErr.Error()})
		logAudit(accountID, "e2e_chat", "llama", r.RemoteAddr, deviceID, "denied", decErr.Error())
		return
	}

	// 防禦層 3：併發上限 → 429。僅對「會生成的串流請求」(stream:true) 設限，保護 GPU 不被灌爆 OOM；
	// n_predict:0 暖機、標題生成等輕量非串流請求不佔名額，避免誤擋正常聊天（它們仍受速率限制保護）。
	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(plaintext, &probe)
	if probe.Stream {
		if !e2eAcquireSlot(accountID) {
			sendJSON(w, http.StatusTooManyRequests, APIResponse{Status: "error", Error: "你已有推論進行中，請待其完成"})
			logAudit(accountID, "e2e_chat", "llama", r.RemoteAddr, deviceID, "denied", "concurrency_limit")
			return
		}
		defer e2eReleaseSlot(accountID)
	}

	// 用同一把 K 建立回應加密用的 GCM
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)

	// 轉發明文請求到 llama.cpp（串流）
	upstream, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8080/v1/chat/completions", bytes.NewReader(plaintext))
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, APIResponse{Status: "error", Error: "建立上游請求失敗"})
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(upstream)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, APIResponse{Status: "error", Error: "無法連接 llama.cpp: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// 決策③：上游非 200 → 明文回傳錯誤（前端 response.ok=false 走既有錯誤處理）
	if resp.StatusCode != http.StatusOK {
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// 成功：逐塊 AES-GCM 加密成 SSE 幀串回（明文只在本機記憶體短暫存在）
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			frame, encErr := encE2EFrame(gcm, buf[:n])
			if encErr != nil {
				return
			}
			if _, werr := io.WriteString(w, "data: "+frame+"\n\n"); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	logAudit(accountID, "e2e_chat", "llama", r.RemoteAddr, deviceID, "success", "e2e_stream")
}

// webuiDistDir 是 forked llama-ui（SvelteKit）的建置產物目錄（index.html / bundle.js / bundle.css）
const webuiDistDir = "./webui/dist"

// 啟動時預先 gzip 前端大檔（bundle.js ~8MB / bundle.css）並快取於記憶體，
// 服務時依 Accept-Encoding 回壓縮版，大幅改善（尤其手機走隧道）首次載入。
type gzAsset struct {
	data    []byte
	modTime time.Time
}

var (
	gzBundleJS  gzAsset
	gzBundleCSS gzAsset
)

func gzipFileToMem(path string) gzAsset {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Printf("⚠️  gzip 預壓失敗，無法讀取 %s: %v", path, err)
		return gzAsset{}
	}
	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	gw.Write(raw)
	gw.Close()
	modTime := time.Now()
	if fi, statErr := os.Stat(path); statErr == nil {
		modTime = fi.ModTime()
	}
	log.Printf("✓ gzip 預壓 %s：%d → %d bytes", path, len(raw), buf.Len())
	return gzAsset{data: buf.Bytes(), modTime: modTime}
}

// initWebuiAssets 啟動時呼叫，預壓前端大檔。
func initWebuiAssets() {
	gzBundleJS = gzipFileToMem(webuiDistDir + "/bundle.js")
	gzBundleCSS = gzipFileToMem(webuiDistDir + "/bundle.css")
}

// serveAsset：客戶端支援 gzip 且已預壓則回壓縮版，否則回原始檔。
// 用 ServeContent + modTime 處理 If-Modified-Since：UI 重建後自動失效、未變回 304，
// 避免開發期 bundle.js 被瀏覽器長期快取而測到舊版。
func serveAsset(w http.ResponseWriter, r *http.Request, path, contentType string, a gzAsset) {
	if a.data != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Encoding", "gzip")
		http.ServeContent(w, r, "", a.modTime, bytes.NewReader(a.data))
		return
	}
	http.ServeFile(w, r, path)
}

// e2eBlockedGenPaths：直接呼叫的「生成類」端點一律封鎖 —— 聊天必須走 /api/e2e/chat
// （E2E 加密 + 速率/重放/併發防禦）。用精確比對，保留 /v1/chat/completions/control（UI 結束推理用）與 metadata。
var e2eBlockedGenPaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/completions":      true,
	"/completions":         true,
	"/completion":          true,
	"/infill":              true,
}

// uiOrProxyHandler：根路徑與前端資源由本地 forked UI 服務；封鎖明文生成後門端點；其餘轉發 llama.cpp。
func uiOrProxyHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		http.ServeFile(w, r, webuiDistDir+"/index.html")
		return
	case "/bundle.js":
		serveAsset(w, r, webuiDistDir+"/bundle.js", "text/javascript; charset=utf-8", gzBundleJS)
		return
	case "/bundle.css":
		serveAsset(w, r, webuiDistDir+"/bundle.css", "text/css; charset=utf-8", gzBundleCSS)
		return
	}

	// 封鎖「直接打的明文生成端點」——繞過 E2E 與 DoS 防禦的後門（紅方在 Burp 證實可用）→ 403。
	if e2eBlockedGenPaths[strings.TrimRight(r.URL.Path, "/")] {
		accountID := r.Header.Get("X-Account-ID")
		log.Printf("⛔ 阻擋明文生成端點直連: account=%s, path=%s\n", accountID, r.URL.Path)
		logAudit(accountID, "blocked_gen_endpoint", r.URL.Path, r.RemoteAddr, r.Header.Get("X-Device-ID"), "denied", "plaintext_generation_bypass")
		sendJSON(w, http.StatusForbidden, APIResponse{Status: "error", Error: "此端點已停用；聊天請改用 E2E 加密端點 /api/e2e/chat"})
		return
	}

	proxyToLlamaAuthenticated(w, r)
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

	// /admin/* 改為需要 X-Admin-Token（由控制面板注入）。未設定時 admin 端點全數鎖定。
	if strings.TrimSpace(os.Getenv("LLAMA_ADMIN_TOKEN")) == "" {
		log.Println("⚠️  LLAMA_ADMIN_TOKEN 未設定，/admin/* 端點已鎖定（僅控制面板可用）")
	}

	// 初始化資料庫
	if err := initDB(); err != nil {
		log.Fatalf("資料庫初始化失敗: %v\n", err)
	}
	defer db.Close()

	// 初始化 E2E RSA-2048 密鑰對（自動載入已有密鑰或生成新密鑰）
	if err := loadOrGenerateRSAKeyPair(); err != nil {
		log.Fatalf("E2E 密鑰初始化失敗: %v\n", err)
	}

	// 預壓前端大檔（gzip），改善 bundle.js 載入速度
	initWebuiAssets()

	// 啟動 E2E 濫用防禦的背景清理（過期的重放 iv / 速率視窗紀錄）
	go e2eCleanupLoop()

	// ==================
	// 路由設定
	// ==================

	// 兩個獨立 mux：
	//  publicMux → :8081（經 Cloudflare Tunnel 對外）
	//  adminMux  → 127.0.0.1:8082（僅本機；tunnel 物理上碰不到 /admin/*）
	publicMux := http.NewServeMux()
	adminMux := http.NewServeMux()

	// 公開端點（無需認證；對外）
	publicMux.HandleFunc("/health", healthHandler)
	publicMux.HandleFunc("/auth/register", registerDeviceHandler)
	publicMux.HandleFunc("/auth/poll", pollStatusHandler)
	publicMux.HandleFunc("/auth/relogin", reloginHandler)                            // 重新登入（自我授權於一次性 code）
	publicMux.HandleFunc("/api/public-key", publicKeyHandler)                        // RSA E2E 公鑰（前端加密用）
	publicMux.HandleFunc("/e2e-test", func(w http.ResponseWriter, r *http.Request) { // E2E 加密測試頁（開發用）
		http.ServeFile(w, r, "e2e_test.html")
	})
	publicMux.HandleFunc("/e2e-test/exchange", e2eExchangeHandler) // 以一次性 code 換 E2E 測試憑證（不入 URL）

	// 需認證的端點（公開 mux）
	publicMux.HandleFunc("/auth/verify", authMiddleware(verifyTokenHandler, "L1"))
	publicMux.HandleFunc("/api/chat", authMiddleware(chatHandler, "L2"))
	publicMux.HandleFunc("/api/e2e/chat", authMiddleware(e2eChatHandler, "L2"))
	publicMux.HandleFunc("/api/conversations", authMiddleware(myConversationsHandler, "L1"))
	publicMux.HandleFunc("/api/llama/", authMiddleware(proxyToLlamaAuthenticated, "L2"))
	// 根路徑與前端資源服務 forked UI；其餘請求轉發 llama.cpp（皆需認證）
	publicMux.HandleFunc("/", authMiddleware(uiOrProxyHandler, "L1"))

	// 管理端點：只在 127.0.0.1:8082 上服務（不對 tunnel 暴露）；仍保留 adminGate 做縱深防禦
	// （擋掉本機其他程序在未持 token 下呼叫）。
	adminMux.HandleFunc("/admin/generate-qr", adminGate(generateQRHandler))
	adminMux.HandleFunc("/admin/pending", adminGate(pendingAccountsHandler))
	adminMux.HandleFunc("/admin/approve", adminGate(approveAccountHandler))
	adminMux.HandleFunc("/admin/accounts", adminGate(listAccountsHandler))
	adminMux.HandleFunc("/admin/logs", adminGate(viewLogsHandler))
	adminMux.HandleFunc("/admin/account-secrets", adminGate(accountSecretsHandler)) // 取得 token+secret（手動）
	adminMux.HandleFunc("/admin/delete-account", adminGate(deleteAccountHandler))   // 刪除帳號
	adminMux.HandleFunc("/admin/relogin-code", adminGate(reloginCodeHandler))       // 為既有帳號產生重新登入連結
	adminMux.HandleFunc("/admin/e2e-code", adminGate(e2eCodeHandler))               // 為 E2E 測試頁產生一次性憑證 code
	adminMux.HandleFunc("/admin/revoke-sessions", adminGate(revokeSessionsHandler)) // 撤銷帳號所有 token（憑證外洩時用）

	// 啟動：管理 API 先在背景起（只綁 loopback），公開服務在前景。
	const adminAddr = "127.0.0.1:8082"
	port := ":8081"

	go func() {
		log.Printf("🔧 管理 API 監聽 %s（僅本機，不對外；/admin/* 仍需 X-Admin-Token）\n", adminAddr)
		if err := http.ListenAndServe(adminAddr, loggingMiddleware(adminMux)); err != nil {
			log.Fatalf("管理 API 啟動失敗: %v\n", err)
		}
	}()

	log.Println("")
	log.Printf("🚀 代理層監聽在 %s（對外，經 Tunnel）\n", port)
	log.Println("")
	log.Println("📌 公開端點:")
	log.Printf("   健康檢查:     GET  http://127.0.0.1%s/health\n", port)
	log.Printf("   設備註冊:     POST http://127.0.0.1%s/auth/register\n", port)
	log.Println("")
	log.Println("🔧 管理端點（僅本機 127.0.0.1:8082，需 X-Admin-Token）:")
	log.Printf("   生成 QR Code: POST http://%s/admin/generate-qr\n", adminAddr)
	log.Printf("   核准帳號:     POST http://%s/admin/approve\n", adminAddr)
	log.Printf("   所有帳號:     GET  http://%s/admin/accounts\n", adminAddr)
	log.Printf("   撤銷登入:     POST http://%s/admin/revoke-sessions\n", adminAddr)
	log.Println("")
	log.Println("🔒 需認證端點（Bearer Token 或 Cookie）:")
	log.Printf("   驗證 Token:   GET  http://127.0.0.1%s/auth/verify\n", port)
	log.Printf("   聊天:         POST http://127.0.0.1%s/api/chat\n", port)
	log.Println("")
	log.Println("🌐 llama.cpp Web UI（需認證）:")
	log.Printf("   http://127.0.0.1%s/  ← 需帶 Authorization: Bearer <token> 或 Cookie\n", port)
	log.Println("")

	if err := http.ListenAndServe(port, loggingMiddleware(publicMux)); err != nil {
		log.Fatalf("啟動失敗: %v\n", err)
	}
}
