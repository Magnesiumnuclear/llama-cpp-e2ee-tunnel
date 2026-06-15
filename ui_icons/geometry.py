# -*- coding: utf-8 -*-
"""繪圖用的相對比例工具。

實踐原則 1「拒絕絕對座標」：所有座標都以畫布 rect 的寬高比例 (0~1) 表示，
視窗縮放時自動跟著縮放，永遠不寫死像素。
"""

from PyQt6.QtCore import QPointF, QRectF, Qt
from PyQt6.QtGui import QPen


class Palette:
    """一個圖示的渲染樣式（與形狀定義分離，呼應原則 3）。

    fill   : 填色用顏色
    stroke : 描邊用顏色
    weight : 線寬，以「較短邊」的比例表示（例如 0.1 = 短邊的 10%）
    """

    __slots__ = ("fill", "stroke", "weight")

    def __init__(self, fill, stroke, weight=0.1):
        self.fill = fill
        self.stroke = stroke
        self.weight = weight


def pt(rect, fx, fy):
    """rect 內以比例 (fx, fy) ∈ [0,1] 表示的點 → 絕對 QPointF。"""
    return QPointF(rect.left() + fx * rect.width(),
                   rect.top() + fy * rect.height())


def square(rect, pad_frac=0.0):
    """取得置中的正方形繪圖區，四周以「較短邊」的比例內縮 pad_frac。

    讓所有圖示在非正方形容器中仍維持等比例、置中。
    """
    side = min(rect.width(), rect.height())
    inner = side * (1.0 - 2.0 * pad_frac)
    cx, cy = rect.center().x(), rect.center().y()
    half = inner / 2.0
    return QRectF(cx - half, cy - half, inner, inner)


def pen(pal, rect, scale=1.0, cap=Qt.PenCapStyle.RoundCap):
    """依 Palette 與畫布短邊比例組出描邊畫筆（圓頭圓角，視覺一致）。"""
    p = QPen(pal.stroke)
    p.setWidthF(pal.weight * min(rect.width(), rect.height()) * scale)
    p.setCapStyle(cap)
    p.setJoinStyle(Qt.PenJoinStyle.RoundJoin)
    return p
