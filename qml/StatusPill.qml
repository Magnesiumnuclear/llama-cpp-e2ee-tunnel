import QtQuick

// 狀態指示：彩色圓點 + 文字；色彩切換帶過渡，idle 狀態有脈動光環。
Row {
    id: pill
    property string label: ""
    property string kind: "bad"   // ok / bad / idle
    property color c: Theme.statusColor(kind)
    spacing: 7

    Behavior on c { ColorAnimation { duration: Theme.animMed } }

    Item {
        width: 12; height: 12
        anchors.verticalCenter: parent.verticalCenter

        Rectangle {
            id: dot
            anchors.centerIn: parent
            width: 11; height: 11; radius: 5.5
            color: pill.c
        }
        // idle 時的脈動光環
        Rectangle {
            id: ring
            anchors.centerIn: parent
            width: 22; height: 22; radius: 11
            color: "transparent"
            border.width: 2
            border.color: pill.c
            visible: pill.kind === "idle"
            SequentialAnimation on opacity {
                running: ring.visible
                loops: Animation.Infinite
                NumberAnimation { from: 0.6; to: 0.0; duration: 1000; easing.type: Easing.OutQuad }
                PauseAnimation { duration: 150 }
            }
            SequentialAnimation on scale {
                running: ring.visible
                loops: Animation.Infinite
                NumberAnimation { from: 0.6; to: 1.3; duration: 1000; easing.type: Easing.OutQuad }
                PauseAnimation { duration: 150 }
            }
        }
    }

    Text {
        anchors.verticalCenter: parent.verticalCenter
        text: pill.label
        color: Theme.text
        font.pixelSize: 13
        font.bold: true
    }
}
