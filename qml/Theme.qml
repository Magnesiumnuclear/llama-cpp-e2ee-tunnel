pragma Singleton
import QtQuick

// 全域設計語彙：色彩、漸層、圓角、動畫時長。
// 集中於此以利「程式重複利用」——所有元件共用同一套視覺常數。
QtObject {
    id: theme

    // ── 背景漸層 ───────────────────────────────────────────
    readonly property color bgTop:    "#0d1024"
    readonly property color bgMid:    "#141a38"
    readonly property color bgBottom: "#1b2148"

    // ── 卡片 ───────────────────────────────────────────────
    readonly property color cardTop:    Qt.rgba(1, 1, 1, 0.07)
    readonly property color cardBottom: Qt.rgba(1, 1, 1, 0.03)
    readonly property color cardBorder: Qt.rgba(1, 1, 1, 0.12)
    readonly property color surface:    Qt.rgba(1, 1, 1, 0.045)
    readonly property color surfaceBorder: Qt.rgba(1, 1, 1, 0.10)

    // ── 文字 ───────────────────────────────────────────────
    readonly property color text:    "#eef1ff"
    readonly property color textDim:  "#9aa3c8"
    readonly property color textFaint:"#6b739c"

    // ── 主題強調色（漸層按鈕）──────────────────────────────
    readonly property color accent1: "#6a8dff"
    readonly property color accent2: "#9b6bff"
    readonly property color danger1: "#ff6a8d"
    readonly property color danger2: "#ff5252"
    readonly property color accentGlow: Qt.rgba(0.42, 0.55, 1.0, 0.45)

    // ── 狀態色 ─────────────────────────────────────────────
    readonly property color ok:   "#43d17a"
    readonly property color bad:  "#ff5d6c"
    readonly property color idle: "#ffc24b"

    // ── 幾何 ───────────────────────────────────────────────
    readonly property int radius:      16
    readonly property int radiusSmall:  10
    readonly property int pad:          18

    // ── 動畫時長（毫秒）────────────────────────────────────
    readonly property int animFast: 130
    readonly property int animMed:  240
    readonly property int animSlow: 420

    function statusColor(kind) {
        if (kind === "ok")   return ok
        if (kind === "idle") return idle
        return bad
    }
}
