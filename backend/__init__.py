# -*- coding: utf-8 -*-
"""llama-proxy 控制面板 — 後端邏輯層。

本套件「不含任何介面程式碼」，與 QML 前端完全解耦：

- proxy_client：純邏輯（HTTP 小工具、常數、cloudflared 偵測），零 Qt GUI 依賴。
- models      ：QML 用的清單資料模型（QAbstractListModel）。
- controller  ：QObject 橋接層，封裝子程序管理與業務流程，對 QML 暴露屬性／訊號／槽。
"""

from .controller import Controller

__all__ = ["Controller"]
