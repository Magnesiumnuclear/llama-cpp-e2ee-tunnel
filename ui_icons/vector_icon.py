# -*- coding: utf-8 -*-
"""VectorIcon：QML 可用的向量圖示元件（以 QPainter 繪製）。

職責單純——把「畫布尺寸 + 樣式(color/strokeColor/weight)」交給 glyphs 中對應 name
的繪製函式。形狀定義在 glyphs（與此處的渲染/屬性管理分離，呼應原則 3）。
"""

from PyQt6.QtCore import QRectF, pyqtProperty
from PyQt6.QtGui import QColor, QPainter
from PyQt6.QtQuick import QQuickPaintedItem
from PyQt6.QtQml import qmlRegisterType

from .geometry import Palette
from .glyphs import GLYPHS


class VectorIcon(QQuickPaintedItem):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._name = ""
        self._color = QColor("#ffffff")
        self._stroke = QColor()        # 預設無效 → 描邊沿用 color
        self._weight = 0.10            # 線寬：短邊比例
        self.setAntialiasing(True)

    def paint(self, painter):
        drawer = GLYPHS.get(self._name)
        if drawer is None:
            return
        painter.setRenderHint(QPainter.RenderHint.Antialiasing, True)
        rect = QRectF(0.0, 0.0, self.width(), self.height())
        stroke = self._stroke if self._stroke.isValid() else self._color
        drawer(painter, rect, Palette(self._color, stroke, self._weight))

    # ── QML 屬性 ────────────────────────────────────────────────
    def _get_name(self):
        return self._name

    def _set_name(self, value):
        if value != self._name:
            self._name = value
            self.update()

    name = pyqtProperty(str, _get_name, _set_name)

    def _get_color(self):
        return self._color

    def _set_color(self, value):
        self._color = value
        self.update()

    color = pyqtProperty(QColor, _get_color, _set_color)

    def _get_stroke(self):
        return self._stroke

    def _set_stroke(self, value):
        self._stroke = value
        self.update()

    strokeColor = pyqtProperty(QColor, _get_stroke, _set_stroke)

    def _get_weight(self):
        return self._weight

    def _set_weight(self, value):
        self._weight = value
        self.update()

    weight = pyqtProperty(float, _get_weight, _set_weight)


def register_icons(uri="App.Icons", major=1, minor=0):
    """把 VectorIcon 註冊給 QML：QML 端 `import App.Icons 1.0` 後可用 VectorIcon{}。"""
    qmlRegisterType(VectorIcon, uri, major, minor, "VectorIcon")
