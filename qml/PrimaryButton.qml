import QtQuick
import QtQuick.Controls.Basic

// 漸層主要按鈕：滑入提亮、按下縮放，皆帶平滑動畫。
Button {
    id: ctrl
    property color c1: Theme.accent1
    property color c2: Theme.accent2

    hoverEnabled: true
    padding: 8
    leftPadding: 18
    rightPadding: 18
    implicitHeight: 40
    font.pixelSize: 14
    font.bold: true

    // 滑鼠懸停時的提亮幅度（0→1），以動畫平滑過渡。
    property real hoverAmt: hovered && enabled ? 1.0 : 0.0
    Behavior on hoverAmt { NumberAnimation { duration: Theme.animFast } }

    opacity: enabled ? 1.0 : 0.4
    Behavior on opacity { NumberAnimation { duration: Theme.animMed } }

    contentItem: Text {
        text: ctrl.text
        color: "white"
        font: ctrl.font
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
    }

    background: Rectangle {
        radius: Theme.radiusSmall
        scale: ctrl.pressed ? 0.96 : 1.0
        Behavior on scale { NumberAnimation { duration: Theme.animFast; easing.type: Easing.OutCubic } }
        gradient: Gradient {
            GradientStop { position: 0.0; color: Qt.lighter(ctrl.c1, 1.0 + 0.14 * ctrl.hoverAmt) }
            GradientStop { position: 1.0; color: Qt.lighter(ctrl.c2, 1.0 + 0.14 * ctrl.hoverAmt) }
        }
    }
}
