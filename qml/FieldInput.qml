import QtQuick
import QtQuick.Controls.Basic

// 統一風格的輸入框：聚焦時邊框過渡至強調色。
TextField {
    id: tf
    color: Theme.text
    placeholderTextColor: Theme.textFaint
    selectionColor: Theme.accent1
    selectedTextColor: "white"
    font.pixelSize: 14
    leftPadding: 12
    rightPadding: 12
    topPadding: 9
    bottomPadding: 9

    background: Rectangle {
        radius: Theme.radiusSmall
        color: Theme.surface
        border.width: 1
        border.color: tf.activeFocus ? Theme.accent1 : Theme.surfaceBorder
        Behavior on border.color { ColorAnimation { duration: Theme.animFast } }
    }
}
