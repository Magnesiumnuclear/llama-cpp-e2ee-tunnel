# -*- coding: utf-8 -*-
"""
llama-proxy 控制面板 (PyQt6)

一鍵：啟動 Cloudflare Tunnel → 自動擷取公網 URL → 帶著 URL 啟動 go 代理層；
並提供 QR 生成預覽、權限審核（核准/拒絕 + L1/L2/L3）、帳號總覽（已通過/未通過）。

依賴：PyQt6（HTTP 走標準庫 urllib，QR 直接顯示 Go 產生的 PNG，故不需 requests/qrcode/PIL）。
啟動：D:\\software\\llama\\.venv\\Scripts\\python.exe control_panel.py
"""

import os
import re
import sys
import json
import glob
import shutil
import subprocess
import urllib.request
import urllib.error

from PyQt6.QtCore import Qt, QProcess, QProcessEnvironment, QTimer, QUrl
from PyQt6.QtGui import QPixmap, QColor, QDesktopServices, QFont
from PyQt6.QtWidgets import (
    QApplication, QMainWindow, QWidget, QLabel, QPushButton, QLineEdit,
    QPlainTextEdit, QVBoxLayout, QHBoxLayout, QGroupBox, QTabWidget,
    QTableWidget, QTableWidgetItem, QComboBox, QHeaderView, QSplitter,
    QMessageBox,
)

# ── 常數 ────────────────────────────────────────────────────────────
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
PROXY_PORT = 8081
PROXY_BASE = f"http://127.0.0.1:{PROXY_PORT}"
LOCAL_TARGET = f"http://localhost:{PROXY_PORT}"
TUNNEL_RE = re.compile(r"https://[a-z0-9-]+\.trycloudflare\.com")
CREATE_NO_WINDOW = 0x08000000  # Windows：呼叫 taskkill 時不彈出視窗

# 帳號狀態 → (顯示文字, 列底色)
STATUS_VIEW = {
    "active":           ("✅ 已通過", "#c8e6c9"),
    "pending_approval": ("⏳ 待審",   "#ffe0b2"),
    "rejected":         ("❌ 未通過", "#ffcdd2"),
    "disabled":         ("⛔ 已停用", "#cfd8dc"),
}


def find_cloudflared():
    fixed = os.path.join(BASE_DIR, "cloudflared-windows-386.exe")
    if os.path.exists(fixed):
        return fixed
    hits = glob.glob(os.path.join(BASE_DIR, "cloudflared*.exe"))
    return hits[0] if hits else None


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


def http_get(path, timeout=5.0):
    req = urllib.request.Request(PROXY_BASE + path, method="GET")
    return _request(req, timeout)


def http_post(path, payload, timeout=8.0):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        PROXY_BASE + path, data=data, method="POST",
        headers={"Content-Type": "application/json"},
    )
    return _request(req, timeout)


# ── 主視窗 ──────────────────────────────────────────────────────────
class ControlPanel(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("llama-proxy 控制面板")
        self.resize(960, 720)

        self.cloudflared = find_cloudflared()
        self.go_path = shutil.which("go") or "go"
        self.public_url = ""
        self.proxy_ready = False
        self.want_proxy = False
        self._tunnel_buf = ""
        self._health_attempts = 0

        # 子程序
        self.tunnel_proc = QProcess(self)
        self.tunnel_proc.setProcessChannelMode(QProcess.ProcessChannelMode.MergedChannels)
        self.tunnel_proc.readyReadStandardOutput.connect(self.on_tunnel_output)
        self.tunnel_proc.errorOccurred.connect(lambda e: self.log_line(f"✗ Tunnel 程序錯誤：{e}"))

        self.proxy_proc = QProcess(self)
        self.proxy_proc.setWorkingDirectory(BASE_DIR)
        self.proxy_proc.setProcessChannelMode(QProcess.ProcessChannelMode.MergedChannels)
        self.proxy_proc.readyReadStandardOutput.connect(self.on_proxy_output)
        self.proxy_proc.errorOccurred.connect(lambda e: self.log_line(f"✗ 代理層程序錯誤：{e}"))

        self.health_timer = QTimer(self)
        self.health_timer.timeout.connect(self.health_tick)

        self._build_ui()

        if not self.cloudflared:
            self.log_line("⚠ 找不到 cloudflared 執行檔（cloudflared*.exe）")
        self.log_line(f"go: {self.go_path}　工作目錄: {BASE_DIR}")

    # ── UI 組裝 ─────────────────────────────────────────────────
    def _build_ui(self):
        central = QWidget()
        self.setCentralWidget(central)
        root = QVBoxLayout(central)

        # 服務啟動列
        svc = QGroupBox("服務")
        sv = QVBoxLayout(svc)
        row1 = QHBoxLayout()
        self.btn_start = QPushButton("▶ 全部啟動")
        self.btn_start.clicked.connect(self.start_all)
        self.btn_stop = QPushButton("■ 停止全部")
        self.btn_stop.clicked.connect(self.stop_all)
        self.tunnel_status = QLabel()
        self.proxy_status = QLabel()
        self.set_status(self.tunnel_status, "未運行", "bad")
        self.set_status(self.proxy_status, "未運行", "bad")
        row1.addWidget(self.btn_start)
        row1.addWidget(self.btn_stop)
        row1.addSpacing(16)
        row1.addWidget(QLabel("Tunnel："))
        row1.addWidget(self.tunnel_status)
        row1.addSpacing(12)
        row1.addWidget(QLabel("代理層："))
        row1.addWidget(self.proxy_status)
        row1.addStretch(1)
        sv.addLayout(row1)

        row2 = QHBoxLayout()
        row2.addWidget(QLabel("公網 URL："))
        self.url_edit = QLineEdit()
        self.url_edit.setReadOnly(True)
        self.url_edit.setPlaceholderText("啟動後自動填入…")
        btn_copy = QPushButton("複製")
        btn_copy.clicked.connect(lambda: QApplication.clipboard().setText(self.url_edit.text()))
        row2.addWidget(self.url_edit, 1)
        row2.addWidget(btn_copy)
        sv.addLayout(row2)
        root.addWidget(svc)

        # 分頁 + log（上下可調）
        splitter = QSplitter(Qt.Orientation.Vertical)

        self.tabs = QTabWidget()
        self.tabs.addTab(self._build_qr_tab(), "QR 生成與預覽")
        self.tabs.addTab(self._build_pending_tab(), "權限審核")
        self.tabs.addTab(self._build_accounts_tab(), "帳號總覽（預覽）")
        self.tabs.setEnabled(False)  # 代理層就緒前停用
        splitter.addWidget(self.tabs)

        self.log = QPlainTextEdit()
        self.log.setReadOnly(True)
        self.log.setFont(QFont("Consolas", 9))
        self.log.setMaximumBlockCount(2000)
        splitter.addWidget(self.log)
        splitter.setSizes([460, 240])
        root.addWidget(splitter, 1)

    def _build_qr_tab(self):
        w = QWidget()
        v = QVBoxLayout(w)
        row = QHBoxLayout()
        row.addWidget(QLabel("account_id："))
        self.qr_account = QLineEdit("user_phone_001")
        row.addWidget(self.qr_account, 1)
        btn = QPushButton("生成 QR")
        btn.clicked.connect(self.gen_qr)
        row.addWidget(btn)
        v.addLayout(row)

        self.qr_label = QLabel("尚未生成")
        self.qr_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.qr_label.setMinimumSize(300, 300)
        self.qr_label.setStyleSheet("border:1px solid #bbb; background:#fafafa; color:#888;")
        v.addWidget(self.qr_label, 1)

        urow = QHBoxLayout()
        urow.addWidget(QLabel("註冊網址："))
        self.qr_url = QLineEdit()
        self.qr_url.setReadOnly(True)
        btn_open = QPushButton("在瀏覽器開啟")
        btn_open.clicked.connect(lambda: self.qr_url.text() and QDesktopServices.openUrl(QUrl(self.qr_url.text())))
        urow.addWidget(self.qr_url, 1)
        urow.addWidget(btn_open)
        v.addLayout(urow)
        self.qr_expire = QLabel("")
        v.addWidget(self.qr_expire)
        return w

    def _build_pending_tab(self):
        w = QWidget()
        v = QVBoxLayout(w)
        top = QHBoxLayout()
        btn = QPushButton("重新整理")
        btn.clicked.connect(self.refresh_pending)
        self.pending_count = QLabel("待審：—")
        top.addWidget(btn)
        top.addWidget(self.pending_count)
        top.addStretch(1)
        v.addLayout(top)

        self.pending_table = QTableWidget(0, 6)
        self.pending_table.setHorizontalHeaderLabels(["帳號", "裝置", "建立時間", "權限", "核准", "拒絕"])
        hh = self.pending_table.horizontalHeader()
        hh.setSectionResizeMode(0, QHeaderView.ResizeMode.Stretch)
        hh.setSectionResizeMode(1, QHeaderView.ResizeMode.Stretch)
        v.addWidget(self.pending_table)
        return w

    def _build_accounts_tab(self):
        w = QWidget()
        v = QVBoxLayout(w)
        top = QHBoxLayout()
        btn = QPushButton("重新整理")
        btn.clicked.connect(self.refresh_accounts)
        self.accounts_count = QLabel("共 —")
        top.addWidget(btn)
        top.addWidget(self.accounts_count)
        top.addStretch(1)
        v.addLayout(top)

        cols = ["帳號", "裝置", "權限", "狀態", "審核結果", "建立時間", "核准時間"]
        self.accounts_table = QTableWidget(0, len(cols))
        self.accounts_table.setHorizontalHeaderLabels(cols)
        hh = self.accounts_table.horizontalHeader()
        hh.setSectionResizeMode(0, QHeaderView.ResizeMode.Stretch)
        hh.setSectionResizeMode(1, QHeaderView.ResizeMode.Stretch)
        v.addWidget(self.accounts_table)
        return w

    # ── 服務啟動／停止 ──────────────────────────────────────────
    def start_all(self):
        if not self.cloudflared:
            self.warn("找不到 cloudflared 執行檔，無法啟動 Tunnel。")
            return
        self.want_proxy = True
        if self.tunnel_proc.state() == QProcess.ProcessState.NotRunning:
            self.public_url = ""
            self._tunnel_buf = ""
            self.url_edit.clear()
            self.set_status(self.tunnel_status, "啟動中…", "idle")
            self.log_line(f"啟動 Tunnel：{self.cloudflared}")
            self.tunnel_proc.start(self.cloudflared, ["tunnel", "--url", LOCAL_TARGET])
        elif self.public_url and self.proxy_proc.state() == QProcess.ProcessState.NotRunning:
            self.start_proxy()

    def start_proxy(self):
        if not self.public_url:
            return
        env = QProcessEnvironment.systemEnvironment()
        env.insert("LLAMA_PUBLIC_URL", self.public_url)
        self.proxy_proc.setProcessEnvironment(env)
        self.set_status(self.proxy_status, "啟動中…（go 編譯中）", "idle")
        self.log_line("啟動代理層：go run main.go")
        self.proxy_proc.start(self.go_path, ["run", "main.go"])
        self._health_attempts = 0
        self.health_timer.start(1000)

    def health_tick(self):
        self._health_attempts += 1
        if self.proxy_proc.state() == QProcess.ProcessState.NotRunning and self._health_attempts > 2:
            self.health_timer.stop()
            self.set_status(self.proxy_status, "啟動失敗", "bad")
            self.log_line("✗ 代理層程序已結束（請看上方 log 找原因）")
            return
        ok, resp = http_get("/health", timeout=1.0)
        if ok and resp.get("status") == "ok":
            self.health_timer.stop()
            self.on_proxy_ready()
        elif self._health_attempts >= 60:
            self.health_timer.stop()
            self.log_line("✗ 等待代理層逾時（60 秒）")

    def on_proxy_ready(self):
        self.proxy_ready = True
        self.set_status(self.proxy_status, "運行中", "ok")
        self.tabs.setEnabled(True)
        self.log_line("✓ 代理層就緒，可開始生成 QR 與審核")
        self.refresh_accounts()
        self.refresh_pending()

    def stop_all(self):
        self.health_timer.stop()
        self.want_proxy = False
        self.proxy_ready = False
        for proc in (self.proxy_proc, self.tunnel_proc):
            if proc.state() != QProcess.ProcessState.NotRunning:
                self._kill_tree(int(proc.processId()))
                proc.kill()
        self.set_status(self.proxy_status, "未運行", "bad")
        self.set_status(self.tunnel_status, "未運行", "bad")
        self.tabs.setEnabled(False)
        self.log_line("已停止所有服務")

    @staticmethod
    def _kill_tree(pid):
        if pid <= 0:
            return
        try:
            subprocess.run(["taskkill", "/F", "/T", "/PID", str(pid)],
                           capture_output=True, creationflags=CREATE_NO_WINDOW)
        except Exception:
            pass

    # ── 子程序輸出 ─────────────────────────────────────────────
    def on_tunnel_output(self):
        chunk = bytes(self.tunnel_proc.readAllStandardOutput()).decode("utf-8", "replace")
        self.append_log("tunnel", chunk)
        if not self.public_url:
            self._tunnel_buf += chunk
            m = TUNNEL_RE.search(self._tunnel_buf)
            if m:
                self.public_url = m.group(0)
                self.url_edit.setText(self.public_url)
                self.set_status(self.tunnel_status, "運行中", "ok")
                self.log_line(f"✓ 取得公網 URL：{self.public_url}")
                if self.want_proxy and self.proxy_proc.state() == QProcess.ProcessState.NotRunning:
                    self.start_proxy()

    def on_proxy_output(self):
        chunk = bytes(self.proxy_proc.readAllStandardOutput()).decode("utf-8", "replace")
        self.append_log("proxy", chunk)

    # ── QR 生成 ─────────────────────────────────────────────────
    def gen_qr(self):
        if not self.proxy_ready:
            self.warn("請先啟動服務（代理層就緒後才能生成 QR）。")
            return
        acc = self.qr_account.text().strip()
        ok, resp = http_post("/admin/generate-qr", {"account_id": acc})
        if not ok or resp.get("status") != "success":
            self.warn(f"生成失敗：{resp.get('error') or resp.get('message')}")
            return
        d = resp.get("data", {})
        png = os.path.join(BASE_DIR, d.get("qr_code_file", ""))
        pix = QPixmap(png)
        if pix.isNull():
            self.qr_label.setText("QR 圖讀取失敗")
            self.log_line(f"✗ 讀不到 QR 圖：{png}")
        else:
            self.qr_label.setPixmap(pix.scaled(
                300, 300, Qt.AspectRatioMode.KeepAspectRatio,
                Qt.TransformationMode.SmoothTransformation))
        base = self.public_url or PROXY_BASE
        reg = f"{base}/auth/register?temp_key={d.get('temp_key','')}&account_id={d.get('account_id','')}"
        self.qr_url.setText(reg)
        self.qr_expire.setText(f"有效期至：{d.get('expires_at','')}")
        self.log_line(f"🔑 QR 生成：{d.get('account_id','')}（有效期 {d.get('expires_at','')}）")

    # ── 權限審核 ───────────────────────────────────────────────
    def refresh_pending(self):
        if not self.proxy_ready:
            return
        ok, resp = http_get("/admin/pending")
        if not ok:
            self.log_line(f"✗ 待審清單讀取失敗：{resp.get('error')}")
            return
        data = resp.get("data") or []
        t = self.pending_table
        t.setRowCount(0)
        for acc in data:
            aid = acc.get("account_id", "")
            r = t.rowCount()
            t.insertRow(r)
            t.setItem(r, 0, self._ro(aid))
            t.setItem(r, 1, self._ro(acc.get("device_id", "")))
            t.setItem(r, 2, self._ro(acc.get("created_at", "")))
            combo = QComboBox()
            combo.addItems(["L1", "L2", "L3"])
            combo.setCurrentText(acc.get("permission") or "L2")
            t.setCellWidget(r, 3, combo)
            b_ok = QPushButton("核准")
            b_ok.clicked.connect(lambda _=False, a=aid, c=combo: self.do_approve(a, c))
            t.setCellWidget(r, 4, b_ok)
            b_no = QPushButton("拒絕")
            b_no.clicked.connect(lambda _=False, a=aid: self.do_reject(a))
            t.setCellWidget(r, 5, b_no)
        self.pending_count.setText(f"待審：{len(data)} 筆")

    def do_approve(self, account_id, combo):
        perm = combo.currentText()
        ok, resp = http_post("/admin/approve",
                             {"account_id": account_id, "permission": perm, "action": "approve"})
        if ok and resp.get("status") == "success":
            tok = (resp.get("data") or {}).get("session_token", "")
            self.log_line(f"✅ 已核准 {account_id}（權限 {perm}）")
            if tok:
                self.log_line(f"   session_token：{tok[:40]}…")
            self.refresh_pending()
            self.refresh_accounts()
        else:
            self.warn(f"核准失敗：{resp.get('error') or resp.get('message')}")

    def do_reject(self, account_id):
        if QMessageBox.question(self, "確認", f"確定拒絕 {account_id}？") != QMessageBox.StandardButton.Yes:
            return
        ok, resp = http_post("/admin/approve",
                             {"account_id": account_id, "action": "reject"})
        if ok and resp.get("status") == "success":
            self.log_line(f"❌ 已拒絕 {account_id}")
            self.refresh_pending()
            self.refresh_accounts()
        else:
            self.warn(f"拒絕失敗：{resp.get('error') or resp.get('message')}")

    # ── 帳號總覽（預覽：已通過/未通過）────────────────────────
    def refresh_accounts(self):
        if not self.proxy_ready:
            return
        ok, resp = http_get("/admin/accounts")
        if not ok:
            self.log_line(f"✗ 帳號清單讀取失敗：{resp.get('error')}")
            return
        data = resp.get("data") or []
        counts = {"active": 0, "pending_approval": 0, "rejected": 0, "disabled": 0}
        t = self.accounts_table
        t.setRowCount(0)
        for acc in data:
            st = acc.get("status", "")
            view, bg = STATUS_VIEW.get(st, (st or "—", "#eceff1"))
            if st in counts:
                counts[st] += 1
            r = t.rowCount()
            t.insertRow(r)
            vals = [acc.get("account_id", ""), acc.get("device_id", ""),
                    acc.get("permission", ""), st, view,
                    acc.get("created_at", ""), acc.get("approved_at", "")]
            for c, val in enumerate(vals):
                it = self._ro(val)
                it.setBackground(QColor(bg))
                t.setItem(r, c, it)
        self.accounts_count.setText(
            f"共 {len(data)}　|　✅ 已通過 {counts['active']}　"
            f"⏳ 待審 {counts['pending_approval']}　"
            f"❌ 未通過 {counts['rejected']}　⛔ 停用 {counts['disabled']}")

    # ── 小工具 ─────────────────────────────────────────────────
    @staticmethod
    def _ro(text):
        it = QTableWidgetItem(str(text))
        it.setFlags(it.flags() & ~Qt.ItemFlag.ItemIsEditable)
        return it

    def set_status(self, label, text, kind):
        color = {"ok": "#2e7d32", "bad": "#c62828", "idle": "#f9a825"}.get(kind, "#666")
        label.setText(f"● {text}")
        label.setStyleSheet(f"color:{color}; font-weight:bold;")

    def append_log(self, tag, text):
        for line in text.splitlines():
            if line.strip():
                self.log.appendPlainText(f"[{tag}] {line}")

    def log_line(self, msg):
        self.log.appendPlainText(msg)

    def warn(self, msg):
        QMessageBox.warning(self, "提醒", msg)

    def closeEvent(self, event):
        self.stop_all()
        event.accept()


def main():
    app = QApplication(sys.argv)
    win = ControlPanel()
    win.show()
    sys.exit(app.exec())


if __name__ == "__main__":
    main()
