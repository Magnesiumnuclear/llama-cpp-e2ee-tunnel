# llama-proxy

> 為 [llama.cpp](https://github.com/ggml-org/llama.cpp) 加上「多用戶認證 + 端到端加密」的 Go 代理層。

本專案是 **llama.cpp 的獨立衍生專案（derivative）**：fork 了 llama.cpp 內建的 Web UI（`tools/ui`，SvelteKit），**只改動 UI 的後端通訊層**，讓聊天內容在前端先加密、經由本專案的 Go 代理層中繼到 llama.cpp，使中間的網路（如 Cloudflare Tunnel）**看不到任何明文對話**。

> ⚠️ 本專案**不含 llama.cpp 本體與模型推理引擎**。你必須**自行安裝並啟動 llama.cpp**（見[前置需求](#前置需求)）。

---

## 這是什麼

```
手機 / 瀏覽器 ──HTTPS 隧道（如 Cloudflare）──→ 代理層 :8081 ──本機──→ llama.cpp :8080
                    （只看得到密文）            認證 / E2E 加解密 / 濫用防禦
```

- **多用戶認證**：QR Code 一次性密鑰 → JWT（90 天）→ HttpOnly Cookie；電腦端核准帳號並設定 L1/L2/L3 權限。
- **端到端加密（E2E）**：聊天的「請求」與「串流回應」雙向加密，中間的隧道/CDN 看不到 prompt 與模型回應。
- **濫用防禦**：速率限制、防重放、推論併發上限，保護本地 GPU。

### ⚠️ 目前限制
- **沒有「伺服器端對話紀錄」功能**：對話只存在瀏覽器本機（IndexedDB），伺服器不保存任何聊天歷史。換瀏覽器/裝置看不到舊對話（可用 UI 內建的 Export/Import JSON 手動搬移）。

---

## 前置需求

| 需求 | 用途 |
|------|------|
| **llama.cpp（已安裝並運行）** | 本專案**連接**它的 server，需先讓 `llama-server` 監聽在 **http://127.0.0.1:8080/** |
| **Go** | 編譯 / 運行代理層 |
| **Node.js 18+** | 建置 fork 的 Web UI |
| Cloudflare Tunnel（cloudflared，可選）| 公網存取（**需自行下載 `.exe`**，見下方）|
| Python 3 + PyQt6（可選）| 一鍵控制面板 |

啟動 llama.cpp 範例：
```bash
# 在 llama.cpp 專案內
./llama-server -m your-model.gguf --host 127.0.0.1 --port 8080
```
> 沒有先啟動 llama.cpp（8080 連不上），代理層雖能啟動，但聊天與 metadata 轉發會失敗。

> **`cloudflared` 需自行下載**（`.gitignore` 排除了 `*.exe`，所以 repo **不含**此執行檔）：到 [Cloudflare cloudflared releases](https://github.com/cloudflare/cloudflared/releases) 下載 Windows 版，放到**專案根目錄**並命名為 `cloudflared-windows-386.exe`（或任何 `cloudflared*.exe`）—— 控制面板會自動偵測。其他平台請下載對應的 cloudflared 執行檔。不需要公網存取（純本機測試）則可略過。

---

## 如何使用

**1. 啟動 llama.cpp** —— 確認 `http://127.0.0.1:8080/` 可連。

**2. 建置 fork 的 Web UI（首次必做）**
```bash
cd webui
npm install
npm run build          # 產出 webui/dist/（由代理層服務）
```

**3. 啟動代理層**
```powershell
# 方式 A：手動
$env:LLAMA_PUBLIC_URL = "https://your-tunnel.trycloudflare.com"   # 本機測試可用 http://127.0.0.1:8081
go run main.go

# 方式 B：控制面板（推薦）—— 自動起 Tunnel、抓公網 URL、帶 URL 啟動代理層，並提供 QR 生成 / 權限審核 / 帳號總覽
python control_panel.py
```

**4. 上線一個使用者**
1. 生成 QR Code（控制面板，或 `POST /admin/generate-qr`）。
2. 手機掃碼 → 顯示「等待核准」。
3. 電腦端核准（控制面板「權限審核」分頁，或 `POST /admin/approve`）。
4. 手機**自動進入**聊天介面 → 開始 E2E 加密對話。

---

## 架構：改動了什麼

本專案 = **Go 代理層（全新）** + **fork 的 llama.cpp Web UI（小幅改後端）**。

### Go 代理層（`main.go`）
夾在手機/瀏覽器與 llama.cpp 之間：
- **認證**：QR 一次性 `temp_key` → 簽發 JWT → 種 HttpOnly Cookie；帳號核准 + L1/L2/L3 權限中間件。
- **服務 fork 的 UI**：`/`、`/bundle.js`、`/bundle.css` 由本地 `webui/dist` 提供（gzip 預壓）。
- **E2E 聊天中繼 `POST /api/e2e/chat`**：解密前端加密請求 → 轉發 llama.cpp `/v1/chat/completions` → 把串流回應**逐塊加密**送回。
- **封鎖明文生成後門**：直接打 `/v1/chat/completions` 等生成端點一律 `403`，強制聊天走加密端點。
- **管理端點**：QR 生成、核准、帳號總覽、審計日誌。
- 其餘 metadata 端點（`/props`、`/v1/models`、`/slots`…）透明轉發 llama.cpp。

### fork 的 Web UI（`webui/`，由 llama.cpp `tools/ui` 複製）
**只改通訊後端，不動渲染與推理邏輯**：

| 檔案 | 改動 |
|------|------|
| `src/lib/constants/api-endpoints.ts` | 聊天端點改打 `/api/e2e/chat`（原為 llama.cpp 的 `/v1/chat/completions`）|
| `src/lib/services/e2e-crypto.ts`（新增）| Web Crypto：加密請求、解密回應 SSE 串流（匯出 `e2eFetch`）|
| `src/lib/services/chat.service.ts` | 兩個聊天 fetch 點改用 `e2eFetch` |
| `src/routes/+layout.svelte` | 加一行啟動 console banner（確認跑的是 fork 版）|

---

## 安全與防禦（怎麼防）

| 防線 | 機制 | 防什麼 |
|------|------|--------|
| **認證** | 每請求需有效 JWT（HttpOnly Cookie / Bearer）+ 帳號須 `active` + L1/L2/L3 權限 | 未授權存取、算力白嫖 |
| **端到端加密** | **請求**：前端產一次性 AES-256 金鑰 K，AES-GCM 加密內容、RSA-OAEP 用伺服器公鑰加密 K。**回應**：代理層用同一把 K 對 llama.cpp 串流**逐塊 AES-GCM 加密**，前端解密。 | 中間隧道 / CDN（如 Cloudflare）竊聽聊天內容 |
| **強制 E2E** | 封鎖直接打的明文生成端點（`/v1/chat/completions` 等 → `403`）| 繞過加密的「明文後門」|
| **速率限制** | 每帳號 60 秒內逾 15 次 → `429` | 連續灌量、單帳號壟斷 GPU |
| **防重放** | 快取近期請求的 `iv`（10 分鐘），重複者 → `409` | 側錄合法封包重放榨乾 GPU |
| **併發上限** | 每帳號同時僅 1 個串流推論 → `429` | GPU 因同時湧入而 OOM / 過熱 |

> **威脅模型**：代理層與 llama.cpp 跑在**你信任的本機**（必須看到明文才能餵模型）。E2E 的目標是讓**不可信的中間網路（Cloudflare Tunnel 等）看不到內容**，而非對你自己的伺服器做到零知識。

更詳細的設計文件見 [`docs/`](docs/)。

---

## License

本專案是 **[llama.cpp](https://github.com/ggml-org/llama.cpp)（ggml-org）的衍生專案**。llama.cpp 採 **MIT License**（Copyright © 2023-2026 The ggml authors）。

- fork 而來的 `webui/` 源自 llama.cpp 的 `tools/ui`，依 MIT 規範**保留原始版權與授權聲明**。
- 本專案的程式碼亦以 **MIT License** 釋出（見 [`LICENSE`](LICENSE)）。
- 依 MIT：可自由修改、商業使用、閉源再發布，唯一義務是**保留原始版權與授權聲明**；軟體按「現況（AS IS）」提供、不附任何擔保。
