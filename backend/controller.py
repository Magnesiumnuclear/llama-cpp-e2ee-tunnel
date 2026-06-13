# -*- coding: utf-8 -*-
"""QObject 橋接層：封裝所有後端流程，對 QML 暴露 屬性 / 訊號 / 槽。

設計原則
========
- 介面（QML）只負責「呈現與互動」，所有業務邏輯都在這裡。
- 對 QML 的 API 表面：
    屬性  : tunnelStatusText/Kind、proxyStatusText/Kind、publicUrl、proxyReady、
            pendingCountText、accountsCountText、qrImageUrl/RegisterUrl/ExpireText/Placeholder、
            cloudflaredMissing、pendingModel、accountsModel
    訊號  : logAppended(str)、warningRaised(str)、qrUpdated()
    槽    : startAll/stopAll、generateQr、refreshPending/refreshAccounts、
            approve/reject/deleteAccount、openE2eTest、openUrl、copyToClipboard、boot

行為與舊版 control_panel.py 完全一致，僅將「呈現」抽離至 QML。
"""

import os
import shutil
import urllib.parse

from PyQt6.QtCore import (
    QObject, QProcess, QProcessEnvironment, QTimer, QUrl,
    pyqtSignal, pyqtSlot, pyqtProperty,
)
from PyQt6.QtGui import QPixmap, QDesktopServices, QGuiApplication

from .proxy_client import (
    BASE_DIR, PROXY_BASE, LOCAL_TARGET, STATUS_VIEW, STATUS_FALLBACK_COLOR,
    TUNNEL_RE, find_cloudflared, kill_tree, http_get, http_post,
)
from .models import DictListModel


class Controller(QObject):
    # ── 對 QML 的訊號 ─────────────────────────────────────────
    logAppended = pyqtSignal(str)
    warningRaised = pyqtSignal(str)
    qrUpdated = pyqtSignal()             # 每次生成都觸發，QML 用來強制重載圖片

    tunnelStatusChanged = pyqtSignal()
    proxyStatusChanged = pyqtSignal()
    publicUrlChanged = pyqtSignal()
    proxyReadyChanged = pyqtSignal()
    pendingCountChanged = pyqtSignal()
    accountsCountChanged = pyqtSignal()
    qrTextChanged = pyqtSignal()
    cloudflaredMissingChanged = pyqtSignal()

    def __init__(self, parent=None):
        super().__init__(parent)

        # 環境
        self.cloudflared = find_cloudflared()
        self.go_path = shutil.which("go") or "go"

        # 狀態旗標
        self._public_url = ""
        self._proxy_ready = False
        self.want_proxy = False
        self._tunnel_buf = ""
        self._health_attempts = 0

        # 狀態列文字 / 種類（ok / bad / idle）
        self._tunnel_text, self._tunnel_kind = "未運行", "bad"
        self._proxy_text, self._proxy_kind = "未運行", "bad"

        # 計數與 QR 文字
        self._pending_count_text = "待審：—"
        self._accounts_count_text = "共 —"
        self._qr_image_url = ""
        self._qr_register_url = ""
        self._qr_expire_text = ""
        self._qr_placeholder = "尚未生成"

        # QML 清單模型
        self._pending_model = DictListModel(
            ["accountId", "deviceId", "createdAt", "permission"], self)
        self._accounts_model = DictListModel(
            ["accountId", "deviceId", "permission", "status", "statusView",
             "statusColor", "createdAt", "approvedAt", "isActive"], self)

        # 子程序：Cloudflare Tunnel
        self.tunnel_proc = QProcess(self)
        self.tunnel_proc.setProcessChannelMode(QProcess.ProcessChannelMode.MergedChannels)
        self.tunnel_proc.readyReadStandardOutput.connect(self.on_tunnel_output)
        self.tunnel_proc.errorOccurred.connect(
            lambda e: self.log_line(f"✗ Tunnel 程序錯誤：{e}"))

        # 子程序：go 代理層
        self.proxy_proc = QProcess(self)
        self.proxy_proc.setWorkingDirectory(BASE_DIR)
        self.proxy_proc.setProcessChannelMode(QProcess.ProcessChannelMode.MergedChannels)
        self.proxy_proc.readyReadStandardOutput.connect(self.on_proxy_output)
        self.proxy_proc.errorOccurred.connect(
            lambda e: self.log_line(f"✗ 代理層程序錯誤：{e}"))

        # 健康檢查輪詢
        self.health_timer = QTimer(self)
        self.health_timer.timeout.connect(self.health_tick)

    # ════════════════════════════════════════════════════════════
    #  QML 可綁定屬性
    # ════════════════════════════════════════════════════════════
    @pyqtProperty(str, notify=tunnelStatusChanged)
    def tunnelStatusText(self):
        return self._tunnel_text

    @pyqtProperty(str, notify=tunnelStatusChanged)
    def tunnelStatusKind(self):
        return self._tunnel_kind

    @pyqtProperty(str, notify=proxyStatusChanged)
    def proxyStatusText(self):
        return self._proxy_text

    @pyqtProperty(str, notify=proxyStatusChanged)
    def proxyStatusKind(self):
        return self._proxy_kind

    @pyqtProperty(str, notify=publicUrlChanged)
    def publicUrl(self):
        return self._public_url

    @pyqtProperty(bool, notify=proxyReadyChanged)
    def proxyReady(self):
        return self._proxy_ready

    @pyqtProperty(str, notify=pendingCountChanged)
    def pendingCountText(self):
        return self._pending_count_text

    @pyqtProperty(str, notify=accountsCountChanged)
    def accountsCountText(self):
        return self._accounts_count_text

    @pyqtProperty(str, notify=qrTextChanged)
    def qrImageUrl(self):
        return self._qr_image_url

    @pyqtProperty(str, notify=qrTextChanged)
    def qrRegisterUrl(self):
        return self._qr_register_url

    @pyqtProperty(str, notify=qrTextChanged)
    def qrExpireText(self):
        return self._qr_expire_text

    @pyqtProperty(str, notify=qrTextChanged)
    def qrPlaceholder(self):
        return self._qr_placeholder

    @pyqtProperty(bool, notify=cloudflaredMissingChanged)
    def cloudflaredMissing(self):
        return self.cloudflared is None

    @pyqtProperty(QObject, constant=True)
    def pendingModel(self):
        return self._pending_model

    @pyqtProperty(QObject, constant=True)
    def accountsModel(self):
        return self._accounts_model

    # ════════════════════════════════════════════════════════════
    #  啟動時的初始訊息（QML 連好訊號後再呼叫）
    # ════════════════════════════════════════════════════════════
    @pyqtSlot()
    def boot(self):
        self.cloudflaredMissingChanged.emit()
        if not self.cloudflared:
            self.log_line("⚠ 找不到 cloudflared 執行檔（cloudflared*.exe）")
        self.log_line(f"go: {self.go_path}　工作目錄: {BASE_DIR}")

    # ════════════════════════════════════════════════════════════
    #  服務啟動／停止
    # ════════════════════════════════════════════════════════════
    @pyqtSlot()
    def startAll(self):
        if not self.cloudflared:
            self.warningRaised.emit("找不到 cloudflared 執行檔，無法啟動 Tunnel。")
            return
        self.want_proxy = True
        if self.tunnel_proc.state() == QProcess.ProcessState.NotRunning:
            self._set_public_url("")
            self._tunnel_buf = ""
            self._set_tunnel_status("啟動中…", "idle")
            self.log_line(f"啟動 Tunnel：{self.cloudflared}")
            self.tunnel_proc.start(self.cloudflared, ["tunnel", "--url", LOCAL_TARGET])
        elif self._public_url and self.proxy_proc.state() == QProcess.ProcessState.NotRunning:
            self.start_proxy()

    def start_proxy(self):
        if not self._public_url:
            return
        env = QProcessEnvironment.systemEnvironment()
        env.insert("LLAMA_PUBLIC_URL", self._public_url)
        self.proxy_proc.setProcessEnvironment(env)
        self._set_proxy_status("啟動中…（go 編譯中）", "idle")
        self.log_line("啟動代理層：go run main.go")
        self.proxy_proc.start(self.go_path, ["run", "main.go"])
        self._health_attempts = 0
        self.health_timer.start(1000)

    def health_tick(self):
        self._health_attempts += 1
        if self.proxy_proc.state() == QProcess.ProcessState.NotRunning and self._health_attempts > 2:
            self.health_timer.stop()
            self._set_proxy_status("啟動失敗", "bad")
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
        self._set_proxy_ready(True)
        self._set_proxy_status("運行中", "ok")
        self.log_line("✓ 代理層就緒，可開始生成 QR 與審核")
        self.refreshAccounts()
        self.refreshPending()

    @pyqtSlot()
    def stopAll(self):
        self.health_timer.stop()
        self.want_proxy = False
        self._set_proxy_ready(False)
        for proc in (self.proxy_proc, self.tunnel_proc):
            if proc.state() != QProcess.ProcessState.NotRunning:
                kill_tree(int(proc.processId()))
                proc.kill()
        self._set_proxy_status("未運行", "bad")
        self._set_tunnel_status("未運行", "bad")
        self.log_line("已停止所有服務")

    # ── 子程序輸出 ─────────────────────────────────────────────
    def on_tunnel_output(self):
        chunk = bytes(self.tunnel_proc.readAllStandardOutput()).decode("utf-8", "replace")
        self.append_log("tunnel", chunk)
        if not self._public_url:
            self._tunnel_buf += chunk
            m = TUNNEL_RE.search(self._tunnel_buf)
            if m:
                self._set_public_url(m.group(0))
                self._set_tunnel_status("運行中", "ok")
                self.log_line(f"✓ 取得公網 URL：{self._public_url}")
                if self.want_proxy and self.proxy_proc.state() == QProcess.ProcessState.NotRunning:
                    self.start_proxy()

    def on_proxy_output(self):
        chunk = bytes(self.proxy_proc.readAllStandardOutput()).decode("utf-8", "replace")
        self.append_log("proxy", chunk)

    # ════════════════════════════════════════════════════════════
    #  QR 生成
    # ════════════════════════════════════════════════════════════
    @pyqtSlot(str)
    def generateQr(self, account_id):
        if not self._proxy_ready:
            self.warningRaised.emit("請先啟動服務（代理層就緒後才能生成 QR）。")
            return
        acc = (account_id or "").strip()
        ok, resp = http_post("/admin/generate-qr", {"account_id": acc})
        if not ok or resp.get("status") != "success":
            self.warningRaised.emit(f"生成失敗：{resp.get('error') or resp.get('message')}")
            return
        d = resp.get("data", {})
        png = os.path.join(BASE_DIR, d.get("qr_code_file", ""))
        pix = QPixmap(png)
        if pix.isNull():
            self._qr_image_url = ""
            self._qr_placeholder = "QR 圖讀取失敗"
            self.log_line(f"✗ 讀不到 QR 圖：{png}")
        else:
            self._qr_image_url = QUrl.fromLocalFile(png).toString()
            self._qr_placeholder = ""
        base = self._public_url or PROXY_BASE
        self._qr_register_url = (
            f"{base}/auth/register?temp_key={d.get('temp_key','')}"
            f"&account_id={d.get('account_id','')}")
        self._qr_expire_text = (
            f"帳號：{d.get('account_id','')}　|　有效期至：{d.get('expires_at','')}")
        self.qrTextChanged.emit()
        self.qrUpdated.emit()
        self.log_line(
            f"🔑 QR 生成：{d.get('account_id','')}（有效期 {d.get('expires_at','')}）")

    # ════════════════════════════════════════════════════════════
    #  權限審核
    # ════════════════════════════════════════════════════════════
    @pyqtSlot()
    def refreshPending(self):
        if not self._proxy_ready:
            return
        ok, resp = http_get("/admin/pending")
        if not ok:
            self.log_line(f"✗ 待審清單讀取失敗：{resp.get('error')}")
            return
        data = resp.get("data") or []
        rows = [{
            "accountId": acc.get("account_id", ""),
            "deviceId": acc.get("device_id", ""),
            "createdAt": acc.get("created_at", ""),
            "permission": acc.get("permission") or "L2",
        } for acc in data]
        self._pending_model.set_rows(rows)
        self._pending_count_text = f"待審：{len(data)} 筆"
        self.pendingCountChanged.emit()

    @pyqtSlot(str, str)
    def approve(self, account_id, permission):
        ok, resp = http_post(
            "/admin/approve",
            {"account_id": account_id, "permission": permission, "action": "approve"})
        if ok and resp.get("status") == "success":
            tok = (resp.get("data") or {}).get("session_token", "")
            self.log_line(f"✅ 已核准 {account_id}（權限 {permission}）")
            if tok:
                self.log_line(f"   session_token：{tok[:40]}…")
            self.refreshPending()
            self.refreshAccounts()
        else:
            self.warningRaised.emit(f"核准失敗：{resp.get('error') or resp.get('message')}")

    @pyqtSlot(str)
    def reject(self, account_id):
        """確認動作由 QML 端負責；此處只執行拒絕。"""
        ok, resp = http_post(
            "/admin/approve", {"account_id": account_id, "action": "reject"})
        if ok and resp.get("status") == "success":
            self.log_line(f"❌ 已拒絕 {account_id}")
            self.refreshPending()
            self.refreshAccounts()
        else:
            self.warningRaised.emit(f"拒絕失敗：{resp.get('error') or resp.get('message')}")

    # ════════════════════════════════════════════════════════════
    #  帳號總覽（已通過 / 未通過）
    # ════════════════════════════════════════════════════════════
    @pyqtSlot()
    def refreshAccounts(self):
        if not self._proxy_ready:
            return
        ok, resp = http_get("/admin/accounts")
        if not ok:
            self.log_line(f"✗ 帳號清單讀取失敗：{resp.get('error')}")
            return
        data = resp.get("data") or []
        counts = {"active": 0, "pending_approval": 0, "rejected": 0, "disabled": 0}
        rows = []
        for acc in data:
            st = acc.get("status", "")
            view, color = STATUS_VIEW.get(st, (st or "—", STATUS_FALLBACK_COLOR))
            if st in counts:
                counts[st] += 1
            rows.append({
                "accountId": acc.get("account_id", ""),
                "deviceId": acc.get("device_id", ""),
                "permission": acc.get("permission", ""),
                "status": st,
                "statusView": view,
                "statusColor": color,
                "createdAt": acc.get("created_at", ""),
                "approvedAt": acc.get("approved_at", ""),
                "isActive": st == "active",
            })
        self._accounts_model.set_rows(rows)
        self._accounts_count_text = (
            f"共 {len(data)}　|　✅ 已通過 {counts['active']}　"
            f"⏳ 待審 {counts['pending_approval']}　"
            f"❌ 未通過 {counts['rejected']}　⛔ 停用 {counts['disabled']}")
        self.accountsCountChanged.emit()

    @pyqtSlot(str)
    def deleteAccount(self, account_id):
        """確認動作由 QML 端負責；此處只執行刪除。"""
        ok, resp = http_post("/admin/delete-account", {"account_id": account_id})
        if ok and resp.get("status") == "success":
            self.log_line(f"🗑 帳號已刪除：{account_id}")
            self.refreshAccounts()
        else:
            self.warningRaised.emit(f"刪除失敗：{resp.get('error') or resp.get('message')}")

    # ── E2E 測試頁開啟 ─────────────────────────────────────────
    @pyqtSlot(str)
    def openE2eTest(self, account_id):
        """呼叫 /admin/account-secrets 取得 token+secret，並在瀏覽器開啟 /e2e-test。"""
        ok, resp = http_get(f"/admin/account-secrets?account_id={account_id}")
        if not ok or resp.get("status") != "success":
            self.warningRaised.emit(f"無法取得帳號憑證：{resp.get('error') or resp.get('message')}")
            return
        d = resp.get("data", {})
        token = d.get("session_token", "")
        secret = d.get("device_secret", "")
        if not token or not secret:
            self.warningRaised.emit("伺服器未回傳 session_token 或 device_secret，無法開啟測試頁。")
            return
        url = (
            f"{PROXY_BASE}/e2e-test"
            f"?token={urllib.parse.quote(token, safe='')}"
            f"&secret={urllib.parse.quote(secret, safe='')}"
        )
        self.log_line(f"🔐 E2E 測試頁：{account_id}")
        QDesktopServices.openUrl(QUrl(url))

    # ════════════════════════════════════════════════════════════
    #  共用小工具（供 QML 呼叫）
    # ════════════════════════════════════════════════════════════
    @pyqtSlot(str)
    def openUrl(self, url):
        if url:
            QDesktopServices.openUrl(QUrl(url))

    @pyqtSlot(str)
    def copyToClipboard(self, text):
        QGuiApplication.clipboard().setText(text or "")

    # ── log ────────────────────────────────────────────────────
    def append_log(self, tag, text):
        for line in text.splitlines():
            if line.strip():
                self.logAppended.emit(f"[{tag}] {line}")

    def log_line(self, msg):
        self.logAppended.emit(msg)

    # ── 內部狀態 setter（同時發出對應 notify 訊號）─────────────
    def _set_public_url(self, url):
        if url != self._public_url:
            self._public_url = url
            self.publicUrlChanged.emit()

    def _set_proxy_ready(self, ready):
        if ready != self._proxy_ready:
            self._proxy_ready = ready
            self.proxyReadyChanged.emit()

    def _set_tunnel_status(self, text, kind):
        self._tunnel_text, self._tunnel_kind = text, kind
        self.tunnelStatusChanged.emit()

    def _set_proxy_status(self, text, kind):
        self._proxy_text, self._proxy_kind = text, kind
        self.proxyStatusChanged.emit()
