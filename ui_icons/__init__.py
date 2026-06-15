# -*- coding: utf-8 -*-
"""向量圖示層（純呈現，零後端依賴）。

以 PyQt 的 2D 繪圖引擎（QPainter）取代介面中的 Unicode 符號。
- geometry：相對比例座標工具（原則 1）+ Palette + 畫筆。
- glyphs  ：每個圖示一個結構化的繪製函式（原則 2/3/4）。
- vector_icon：VectorIcon(QQuickPaintedItem) — QML 可用的圖示元件，並負責註冊型別。

與邏輯（backend/）完全分離：本套件不 import backend，backend 也不 import 本套件。
"""

from .vector_icon import VectorIcon, register_icons

__all__ = ["VectorIcon", "register_icons"]
