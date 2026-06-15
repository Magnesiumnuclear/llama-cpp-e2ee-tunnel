# -*- coding: utf-8 -*-
"""向量圖示的形狀定義。

每個圖示一個 draw_*(painter, rect, pal) 函式，遵循：
  原則 1：座標全用 pt()/square() 的「相對比例」，不寫死像素。
  原則 2：每個關鍵點先以具名比例變數表達其意義，不用魔術數字。
  原則 3：先用 QPainterPath 把「形狀」建好，再交給 painter 一次填色/描邊。
  原則 4：複雜圖示拆成多個私有輔助函式 _draw_*()。

GLYPHS：名稱 → 繪製函式，供 VectorIcon 依 name 分派。
"""

import math

from PyQt6.QtCore import Qt, QRectF, QPointF
from PyQt6.QtGui import QPainterPath

from .geometry import pt, square, pen


# ── 共用小工具 ──────────────────────────────────────────────────────
def _stroke(painter, pal, rect, scale=1.0):
    """切到描邊模式（無填色）。"""
    painter.setPen(pen(pal, rect, scale))
    painter.setBrush(Qt.BrushStyle.NoBrush)


def _rounded_rect_path(s, x0, y0, x1, y1, radius_frac=0.16):
    """以比例座標 (x0,y0)-(x1,y1) 建一個圓角矩形路徑。"""
    box = QRectF(pt(s, x0, y0), pt(s, x1, y1))
    rx = box.width() * radius_frac
    ry = box.height() * radius_frac
    path = QPainterPath()
    path.addRoundedRect(box, rx, ry)
    return path


# ── 媒體控制：播放 / 停止 ────────────────────────────────────────────
def draw_play(painter, rect, pal):
    s = square(rect, 0.16)
    back_x, tip_x = 0.18, 0.90      # 三角形：左側底邊 x、右側尖端 x
    top_y, bot_y, mid_y = 0.12, 0.88, 0.50
    path = QPainterPath()
    path.moveTo(pt(s, back_x, top_y))
    path.lineTo(pt(s, tip_x, mid_y))
    path.lineTo(pt(s, back_x, bot_y))
    path.closeSubpath()
    painter.fillPath(path, pal.fill)


def draw_stop(painter, rect, pal):
    s = square(rect, 0.24)
    corner_frac = 0.18
    painter.fillPath(_rounded_rect_path(s, 0.0, 0.0, 1.0, 1.0, corner_frac), pal.fill)


# ── 動作：複製 / 重新登入 / 鎖 / 刪除 / 鑰匙 / 齒輪 ────────────────────
def draw_copy(painter, rect, pal):
    s = square(rect, 0.16)
    _stroke(painter, pal, rect, 0.9)
    back_sheet = (0.30, 0.06, 0.94, 0.70)    # 後方文件 (左,上,右,下) 比例
    front_sheet = (0.06, 0.30, 0.70, 0.94)   # 前方文件
    painter.drawPath(_rounded_rect_path(s, *back_sheet))
    painter.drawPath(_rounded_rect_path(s, *front_sheet))


def draw_relogin(painter, rect, pal):
    """循環箭頭（↻）：一段開口圓弧 + 弧端的箭頭。"""
    s = square(rect, 0.20)
    _stroke(painter, pal, rect, 1.0)
    box = QRectF(s)
    start_deg, sweep_deg = 125.0, 280.0   # 逆時針掃 280°，右側留開口
    arc = QPainterPath()
    arc.arcMoveTo(box, start_deg)
    arc.arcTo(box, start_deg, sweep_deg)
    painter.drawPath(arc)
    _draw_arrow_head(painter, s, pal, rect, start_deg + sweep_deg)


def _draw_arrow_head(painter, s, pal, rect, end_deg):
    """在圓弧端點、沿切線方向畫一個填色三角箭頭。"""
    cx, cy = s.center().x(), s.center().y()
    radius = s.width() / 2.0
    end = math.radians(end_deg)
    tip = QPointF(cx + radius * math.cos(end), cy - radius * math.sin(end))
    tangent = end_deg - 90.0                       # 逆時針前進方向
    wing = min(rect.width(), rect.height()) * 0.22
    spread = 26.0
    a1 = math.radians(tangent + 180 + spread)
    a2 = math.radians(tangent + 180 - spread)
    head = QPainterPath()
    head.moveTo(tip)
    head.lineTo(QPointF(tip.x() + wing * math.cos(a1), tip.y() - wing * math.sin(a1)))
    head.lineTo(QPointF(tip.x() + wing * math.cos(a2), tip.y() - wing * math.sin(a2)))
    head.closeSubpath()
    painter.fillPath(head, pal.stroke)


def draw_lock(painter, rect, pal):
    s = square(rect, 0.16)
    _stroke(painter, pal, rect, 0.95)
    _draw_lock_shackle(painter, s)
    _draw_lock_body(painter, s, pal)


def _draw_lock_shackle(painter, s):
    """鎖的上半部 ㄇ 形提環。"""
    arch = QRectF(pt(s, 0.28, 0.06), pt(s, 0.72, 0.52))
    path = QPainterPath()
    path.moveTo(pt(s, 0.30, 0.50))
    path.lineTo(pt(s, 0.30, 0.30))
    path.arcTo(arch, 180.0, -180.0)   # 上半圓
    path.lineTo(pt(s, 0.70, 0.50))
    painter.drawPath(path)


def _draw_lock_body(painter, s, pal):
    """鎖身（描邊圓角矩形）+ 鑰匙孔。"""
    body_box = (0.16, 0.48, 0.84, 0.94)      # 鎖身範圍 (左,上,右,下)
    body_corner = 0.14                        # 圓角（邊長比例）
    keyhole_center = (0.50, 0.68)
    keyhole_r = 0.05                           # 鑰匙孔半徑（短邊比例）
    painter.drawPath(_rounded_rect_path(s, *body_box, body_corner))
    keyhole = QPainterPath()
    keyhole.addEllipse(pt(s, *keyhole_center), s.width() * keyhole_r, s.height() * keyhole_r)
    painter.fillPath(keyhole, pal.stroke)


def draw_trash(painter, rect, pal):
    s = square(rect, 0.16)
    _stroke(painter, pal, rect, 0.95)
    _draw_trash_lid(painter, s)
    _draw_trash_can(painter, s)


def _draw_trash_lid(painter, s):
    lid = QPainterPath()
    lid.moveTo(pt(s, 0.14, 0.24))
    lid.lineTo(pt(s, 0.86, 0.24))     # 蓋板橫桿
    handle_top, handle_lift = 0.12, 0.24
    lid.moveTo(pt(s, 0.40, handle_lift))
    lid.lineTo(pt(s, 0.44, handle_top))
    lid.lineTo(pt(s, 0.56, handle_top))
    lid.lineTo(pt(s, 0.60, handle_lift))
    painter.drawPath(lid)


def _draw_trash_can(painter, s):
    can = QPainterPath()
    can.moveTo(pt(s, 0.22, 0.30))
    can.lineTo(pt(s, 0.28, 0.90))
    can.lineTo(pt(s, 0.72, 0.90))
    can.lineTo(pt(s, 0.78, 0.30))     # 桶身梯形
    painter.drawPath(can)
    ribs = QPainterPath()
    for fx in (0.40, 0.50, 0.60):     # 內部三條直紋
        ribs.moveTo(pt(s, fx, 0.40))
        ribs.lineTo(pt(s, fx, 0.80))
    painter.drawPath(ribs)


def draw_key(painter, rect, pal):
    s = square(rect, 0.18)
    _stroke(painter, pal, rect, 0.9)
    ring = QPainterPath()
    ring.addEllipse(QRectF(pt(s, 0.08, 0.12), pt(s, 0.48, 0.52)))   # 環
    painter.drawPath(ring)
    shaft = QPainterPath()
    shaft.moveTo(pt(s, 0.44, 0.48))
    shaft.lineTo(pt(s, 0.88, 0.88))                                 # 桿
    shaft.moveTo(pt(s, 0.74, 0.74))
    shaft.lineTo(pt(s, 0.84, 0.64))                                 # 齒 1
    shaft.moveTo(pt(s, 0.64, 0.64))
    shaft.lineTo(pt(s, 0.74, 0.54))                                 # 齒 2
    painter.drawPath(shaft)


def draw_gear(painter, rect, pal):
    s = square(rect, 0.12)
    _stroke(painter, pal, rect, 0.9)
    painter.drawPath(_gear_path(s))
    hole_r = s.width() * 0.14        # 中央孔半徑
    hole = QPainterPath()
    hole.addEllipse(s.center(), hole_r, hole_r)
    painter.drawPath(hole)


def _gear_path(s):
    """在外圈半徑與齒根半徑之間交替取點，組成齒輪輪廓。"""
    cx, cy = s.center().x(), s.center().y()
    r_tip, r_root = s.width() * 0.46, s.width() * 0.34
    teeth = 8
    steps = teeth * 2
    path = QPainterPath()
    for i in range(steps + 1):
        ang = 2.0 * math.pi * i / steps
        r = r_tip if i % 2 == 0 else r_root
        p = QPointF(cx + r * math.cos(ang), cy + r * math.sin(ang))
        path.moveTo(p) if i == 0 else path.lineTo(p)
    path.closeSubpath()
    return path


# ── 建置狀態：閃電（沿用）/ 鐵鎚（重新編譯）──────────────────────────
def draw_bolt(painter, rect, pal):
    s = square(rect, 0.16)
    path = QPainterPath()
    path.moveTo(pt(s, 0.56, 0.04))
    path.lineTo(pt(s, 0.22, 0.56))
    path.lineTo(pt(s, 0.46, 0.56))
    path.lineTo(pt(s, 0.40, 0.96))
    path.lineTo(pt(s, 0.80, 0.40))
    path.lineTo(pt(s, 0.54, 0.40))
    path.closeSubpath()
    painter.fillPath(path, pal.fill)


def draw_hammer(painter, rect, pal):
    s = square(rect, 0.14)
    _stroke(painter, pal, rect, 1.1)
    handle = QPainterPath()
    handle.moveTo(pt(s, 0.46, 0.40))
    handle.lineTo(pt(s, 0.86, 0.86))      # 柄
    painter.drawPath(handle)
    head = QPainterPath()                  # 鎚頭（填色四邊形）
    head.moveTo(pt(s, 0.14, 0.32))
    head.lineTo(pt(s, 0.50, 0.10))
    head.lineTo(pt(s, 0.66, 0.32))
    head.lineTo(pt(s, 0.30, 0.54))
    head.closeSubpath()
    painter.fillPath(head, pal.fill)


# ── 記錄嚴重度 / 狀態：勾 / 叉 / 警告 / 沙漏 / 禁止 / 圈勾 / 圈叉 / 點 ──
def draw_check(painter, rect, pal):
    s = square(rect, 0.18)
    _stroke(painter, pal, rect, 1.2)
    path = QPainterPath()
    path.moveTo(pt(s, 0.14, 0.54))
    path.lineTo(pt(s, 0.40, 0.80))
    path.lineTo(pt(s, 0.88, 0.20))
    painter.drawPath(path)


def draw_cross(painter, rect, pal):
    s = square(rect, 0.24)
    _stroke(painter, pal, rect, 1.2)
    path = QPainterPath()
    path.moveTo(pt(s, 0.14, 0.14))
    path.lineTo(pt(s, 0.86, 0.86))
    path.moveTo(pt(s, 0.86, 0.14))
    path.lineTo(pt(s, 0.14, 0.86))
    painter.drawPath(path)


def draw_warning(painter, rect, pal):
    s = square(rect, 0.10)
    _stroke(painter, pal, rect, 0.85)
    triangle = QPainterPath()
    triangle.moveTo(pt(s, 0.50, 0.10))
    triangle.lineTo(pt(s, 0.95, 0.86))
    triangle.lineTo(pt(s, 0.05, 0.86))
    triangle.closeSubpath()
    painter.drawPath(triangle)
    _draw_exclaim(painter, s, pal)


def _draw_exclaim(painter, s, pal):
    center_x = 0.50
    stem_top, stem_bot = 0.40, 0.62
    dot_y = 0.74
    dot_r = 0.035                     # 驚嘆號圓點半徑（短邊比例）
    stem = QPainterPath()
    stem.moveTo(pt(s, center_x, stem_top))
    stem.lineTo(pt(s, center_x, stem_bot))
    painter.drawPath(stem)
    dot = QPainterPath()
    dot.addEllipse(pt(s, center_x, dot_y), s.width() * dot_r, s.width() * dot_r)
    painter.fillPath(dot, pal.stroke)


def draw_hourglass(painter, rect, pal):
    s = square(rect, 0.20)
    _stroke(painter, pal, rect, 0.9)
    caps = QPainterPath()
    caps.moveTo(pt(s, 0.18, 0.08)); caps.lineTo(pt(s, 0.82, 0.08))   # 頂蓋
    caps.moveTo(pt(s, 0.18, 0.92)); caps.lineTo(pt(s, 0.82, 0.92))   # 底座
    painter.drawPath(caps)
    glass = QPainterPath()
    waist_y = 0.50
    glass.moveTo(pt(s, 0.24, 0.08)); glass.lineTo(pt(s, 0.76, 0.08))
    glass.lineTo(pt(s, 0.50, waist_y)); glass.closeSubpath()         # 上三角
    glass.moveTo(pt(s, 0.24, 0.92)); glass.lineTo(pt(s, 0.76, 0.92))
    glass.lineTo(pt(s, 0.50, waist_y)); glass.closeSubpath()         # 下三角
    painter.drawPath(glass)


def draw_ban(painter, rect, pal):
    s = square(rect, 0.14)
    _stroke(painter, pal, rect, 0.95)
    circle = QPainterPath()
    circle.addEllipse(QRectF(s))
    painter.drawPath(circle)
    slash = QPainterPath()
    slash.moveTo(pt(s, 0.24, 0.24))
    slash.lineTo(pt(s, 0.76, 0.76))
    painter.drawPath(slash)


def _circle_with(painter, rect, pal, inner_builder):
    """畫一個外圈，再交給 inner_builder 在圈內補上勾/叉（共用於狀態圖示）。"""
    s = square(rect, 0.12)
    _stroke(painter, pal, rect, 0.85)
    circle = QPainterPath()
    circle.addEllipse(QRectF(s))
    painter.drawPath(circle)
    painter.drawPath(inner_builder(s))


def draw_check_circle(painter, rect, pal):
    def inner(s):
        path = QPainterPath()
        path.moveTo(pt(s, 0.30, 0.52))
        path.lineTo(pt(s, 0.45, 0.68))
        path.lineTo(pt(s, 0.72, 0.34))
        return path
    _circle_with(painter, rect, pal, inner)


def draw_cross_circle(painter, rect, pal):
    def inner(s):
        path = QPainterPath()
        path.moveTo(pt(s, 0.36, 0.36)); path.lineTo(pt(s, 0.64, 0.64))
        path.moveTo(pt(s, 0.64, 0.36)); path.lineTo(pt(s, 0.36, 0.64))
        return path
    _circle_with(painter, rect, pal, inner)


def draw_dot(painter, rect, pal):
    s = square(rect, 0.36)
    path = QPainterPath()
    path.addEllipse(QRectF(s))
    painter.fillPath(path, pal.fill)


# ── 名稱 → 繪製函式 ─────────────────────────────────────────────────
GLYPHS = {
    "play": draw_play,
    "stop": draw_stop,
    "copy": draw_copy,
    "relogin": draw_relogin,
    "lock": draw_lock,
    "trash": draw_trash,
    "key": draw_key,
    "gear": draw_gear,
    "bolt": draw_bolt,
    "hammer": draw_hammer,
    "check": draw_check,
    "cross": draw_cross,
    "warning": draw_warning,
    "hourglass": draw_hourglass,
    "ban": draw_ban,
    "check_circle": draw_check_circle,
    "cross_circle": draw_cross_circle,
    "dot": draw_dot,
}
