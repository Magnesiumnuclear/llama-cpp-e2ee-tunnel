import QtQuick
import QtQuick.Layouts

// 區段標題：漸層強調條 + 標題文字。
RowLayout {
    property string title: ""
    spacing: 10

    Rectangle {
        Layout.preferredWidth: 4
        Layout.preferredHeight: 18
        radius: 2
        gradient: Gradient {
            GradientStop { position: 0.0; color: Theme.accent1 }
            GradientStop { position: 1.0; color: Theme.accent2 }
        }
    }
    Text {
        text: title
        color: Theme.text
        font.pixelSize: 16
        font.bold: true
    }
}
