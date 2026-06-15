# -*- coding: utf-8 -*-
"""llama-proxy 控制面板 — 進入點（Qt Quick / QML 版）。

一鍵：啟動 Cloudflare Tunnel → 自動擷取公網 URL → 帶著 URL 啟動 go 代理層；
並提供 QR 生成預覽、權限審核（核准/拒絕 + L1/L2/L3）、帳號總覽（已通過/未通過）。

架構（前後端完美分離）
======================
- backend/ ：純後端邏輯與 QObject 橋接層（無任何介面程式碼）。
- qml/     ：Qt Quick 介面（動畫、漸層、滑動），僅負責呈現與互動。
- 本檔     ：薄薄的組裝層——建立 App、載入 QML、把 Controller 注入 QML 環境。

依賴：PyQt6（含 QtQuick / QtQml）。HTTP 走標準庫 urllib。
啟動：D:\\software\\llama\\.venv\\Scripts\\python.exe control_panel.py
"""

import os
import sys

# 必須在建立 QGuiApplication 之前指定樣式，才能完整自訂 Controls 外觀。
os.environ.setdefault("QT_QUICK_CONTROLS_STYLE", "Basic")

from PyQt6.QtCore import QUrl
from PyQt6.QtGui import QGuiApplication
from PyQt6.QtQml import QQmlApplicationEngine

from backend import Controller
from ui_icons import register_icons

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
QML_MAIN = os.path.join(BASE_DIR, "qml", "Main.qml")


def main():
    app = QGuiApplication(sys.argv)
    app.setApplicationName("llama-proxy 控制面板")

    # 註冊向量圖示型別給 QML（import App.Icons 1.0 → VectorIcon{}）
    register_icons()

    controller = Controller()

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()
    ctx.setContextProperty("backend", controller)
    ctx.setContextProperty("pendingModel", controller.pendingModel)
    ctx.setContextProperty("accountsModel", controller.accountsModel)

    engine.load(QUrl.fromLocalFile(QML_MAIN))
    if not engine.rootObjects():
        sys.exit(1)

    # 視窗關閉以外的退出路徑也要確保子程序被收乾淨。
    app.aboutToQuit.connect(controller.stopAll)

    # QML 已連好訊號，補送啟動訊息。
    controller.boot()

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
