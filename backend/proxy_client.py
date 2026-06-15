# -*- coding: utf-8 -*-
"""純邏輯層：常數、cloudflared 偵測、localhost HTTP 小工具。

這個模組刻意「不依賴任何 GUI（Qt Widgets / QML）」，方便重複利用與單元測試。
所有與 llama-proxy 後端溝通的 HTTP 行為都集中在此。
"""

import os
import re
import glob
import json
import subprocess
import urllib.request
import urllib.error

# ── 常數 ────────────────────────────────────────────────────────────
# 專案根目錄＝本檔（backend/proxy_client.py）的上一層，與舊版 control_panel.py 同層。
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PROXY_PORT = 8081
PROXY_BASE = f"http://127.0.0.1:{PROXY_PORT}"
LOCAL_TARGET = f"http://localhost:{PROXY_PORT}"
TUNNEL_RE = re.compile(r"https://[a-z0-9-]+\.trycloudflare\.com")
CREATE_NO_WINDOW = 0x08000000  # Windows：呼叫 taskkill 時不彈出視窗

# 帳號狀態 → (顯示文字, 代表色)。
# 不含任何符號：圖示由 UI 依「狀態鍵」（active/pending_approval/...）自行繪製，邏輯與呈現分離。
STATUS_VIEW = {
    "active":           ("已通過", "#43a047"),
    "pending_approval": ("待審",   "#fb8c00"),
    "rejected":         ("未通過", "#e53935"),
    "disabled":         ("已停用", "#78909c"),
}
STATUS_FALLBACK_COLOR = "#90a4ae"

# 代理層編譯快取：go build 的輸出檔，與「任一比它新就需重編」的來源檔。
PROXY_EXE = "llama-proxy.exe"
BUILD_SOURCES = ("main.go", "go.mod", "go.sum")


def rebuild_reason(base_dir, exe_name=PROXY_EXE, sources=BUILD_SOURCES):
    """判斷是否需要重新編譯。

    需要時回傳「原因字串」（供 log 顯示），不需要時回傳 None。
    規則：編譯結果不存在，或任一來源檔（main.go 等）的修改時間比它新。
    """
    exe_path = os.path.join(base_dir, exe_name)
    if not os.path.exists(exe_path):
        return f"{exe_name} 不存在"
    try:
        exe_mtime = os.path.getmtime(exe_path)
    except OSError:
        return f"無法讀取 {exe_name} 的時間"
    for src in sources:
        src_path = os.path.join(base_dir, src)
        if not os.path.exists(src_path):
            continue
        try:
            if os.path.getmtime(src_path) > exe_mtime:
                return f"{src} 已變動"
        except OSError:
            return f"無法讀取 {src} 的時間"
    return None


def find_cloudflared():
    fixed = os.path.join(BASE_DIR, "cloudflared-windows-386.exe")
    if os.path.exists(fixed):
        return fixed
    hits = glob.glob(os.path.join(BASE_DIR, "cloudflared*.exe"))
    return hits[0] if hits else None


def kill_tree(pid):
    """強制終止整個程序樹（Windows）。"""
    if pid <= 0:
        return
    try:
        subprocess.run(["taskkill", "/F", "/T", "/PID", str(pid)],
                       capture_output=True, creationflags=CREATE_NO_WINDOW)
    except Exception:
        pass


# ── HTTP 小工具（localhost、同步、毫秒級）──────────────────────────
def _request(req, timeout):
    """回傳 (ok, parsed_json)。HTTPError 也會嘗試解析伺服器的 JSON 錯誤內容。"""
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return True, json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        try:
            return False, json.loads(e.read().decode("utf-8"))
        except Exception:
            return False, {"error": f"HTTP {e.code}"}
    except Exception as e:
        return False, {"error": str(e)}


# /admin/* 端點的授權 token（由控制面板啟動時隨機產生，同時注入 proxy 的 LLAMA_ADMIN_TOKEN env）。
# 隨每個請求帶上 X-Admin-Token；公網 tunnel 攻擊者不知此值，故無法呼叫 /admin/*。
ADMIN_TOKEN = ""


def set_admin_token(token):
    global ADMIN_TOKEN
    ADMIN_TOKEN = token or ""


def _add_admin_header(req):
    if ADMIN_TOKEN:
        req.add_header("X-Admin-Token", ADMIN_TOKEN)
    return req


def http_get(path, timeout=5.0):
    req = urllib.request.Request(PROXY_BASE + path, method="GET")
    return _request(_add_admin_header(req), timeout)


def http_post(path, payload, timeout=8.0):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        PROXY_BASE + path, data=data, method="POST",
        headers={"Content-Type": "application/json"},
    )
    return _request(_add_admin_header(req), timeout)
