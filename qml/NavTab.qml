import QtQuick
import QtQuick.Controls.Basic

// 分頁標籤：選中時漸層膠囊高亮、文字過渡。
TabButton {
    id: tb
    implicitHeight: 40
    font.pixelSize: 14

    contentItem: Text {
        text: tb.text
        color: tb.checked ? Theme.text : Theme.textDim
        font.pixelSize: 14
        font.bold: tb.checked
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
        Behavior on color { ColorAnimation { duration: Theme.animFast } }
    }

    background: Rectangle {
        radius: Theme.radiusSmall
        color: "transparent"

        Rectangle {
            anchors.fill: parent
            radius: Theme.radiusSmall
            opacity: tb.checked ? 1.0 : (tb.hovered ? 0.5 : 0.0)
            border.width: tb.checked ? 1 : 0
            border.color: Qt.rgba(Theme.accent1.r, Theme.accent1.g, Theme.accent1.b, 0.5)
            gradient: Gradient {
                GradientStop { position: 0.0; color: Qt.rgba(Theme.accent1.r, Theme.accent1.g, Theme.accent1.b, 0.22) }
                GradientStop { position: 1.0; color: Qt.rgba(Theme.accent2.r, Theme.accent2.g, Theme.accent2.b, 0.22) }
            }
            Behavior on opacity { NumberAnimation { duration: Theme.animFast } }
        }
    }
}
