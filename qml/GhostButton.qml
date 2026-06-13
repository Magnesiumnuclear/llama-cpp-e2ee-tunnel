import QtQuick
import QtQuick.Controls.Basic

// 次要按鈕：透明描邊、滑入加深填色、按下縮放。可切換 danger 配色。
Button {
    id: ctrl
    property bool danger: false
    property color accent: danger ? Theme.danger2 : Theme.accent1

    hoverEnabled: true
    padding: 7
    leftPadding: 16
    rightPadding: 16
    implicitHeight: 36
    font.pixelSize: 13
    font.bold: true

    property real hoverAmt: hovered && enabled ? 1.0 : 0.0
    Behavior on hoverAmt { NumberAnimation { duration: Theme.animFast } }

    opacity: enabled ? 1.0 : 0.4
    Behavior on opacity { NumberAnimation { duration: Theme.animMed } }

    contentItem: Text {
        text: ctrl.text
        color: ctrl.accent
        font: ctrl.font
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
    }

    background: Rectangle {
        radius: Theme.radiusSmall
        color: Qt.rgba(ctrl.accent.r, ctrl.accent.g, ctrl.accent.b, 0.08 + 0.14 * ctrl.hoverAmt)
        border.width: 1
        border.color: Qt.rgba(ctrl.accent.r, ctrl.accent.g, ctrl.accent.b, 0.45)
        scale: ctrl.pressed ? 0.96 : 1.0
        Behavior on scale { NumberAnimation { duration: Theme.animFast; easing.type: Easing.OutCubic } }
    }
}
