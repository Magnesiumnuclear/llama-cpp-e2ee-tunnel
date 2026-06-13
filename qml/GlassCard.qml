import QtQuick

// 可重複利用的「玻璃卡片」容器：圓角、漸層、細邊框。
// 內容寫在元件內部即自動放入有內距的內容區（body）。
Item {
    id: root
    default property alias content: body.data
    property int padding: Theme.pad
    property color tintTop: Theme.cardTop
    property color tintBottom: Theme.cardBottom
    property color borderColor: Theme.cardBorder

    // 內容導向尺寸（外層若以 Layout 指定大小則此值僅作後備，不會造成迴圈）
    implicitWidth: body.childrenRect.width + padding * 2
    implicitHeight: body.childrenRect.height + padding * 2

    Rectangle {
        anchors.fill: parent
        radius: Theme.radius
        border.width: 1
        border.color: root.borderColor
        gradient: Gradient {
            GradientStop { position: 0.0; color: root.tintTop }
            GradientStop { position: 1.0; color: root.tintBottom }
        }
    }

    Item {
        id: body
        anchors.fill: parent
        anchors.margins: root.padding
    }
}
