import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 重新登入結果對話框：顯示一次性連結 + QR；以「複製連結」為主、「在此電腦開啟」為輔。
// 把連結／QR 交給使用者，在新網址開啟即以原帳號登入（保留對話）。
Popup {
    id: dlg
    modal: true
    dim: true
    width: 420
    anchors.centerIn: Overlay.overlay
    padding: 0
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    // 由 Main 在 backend.reloginReady 時呼叫：強制重載 QR 圖再開啟。
    function showResult() {
        reImg.source = ""
        reImg.source = backend.reloginQrImageUrl
        open()
    }

    Overlay.modal: Rectangle { color: Qt.rgba(0, 0, 0, 0.55) }

    background: Rectangle {
        radius: Theme.radius
        color: "#1b2046"
        border.width: 1
        border.color: Theme.cardBorder
    }

    enter: Transition {
        ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0.0; to: 1.0; duration: Theme.animMed }
            NumberAnimation { property: "scale"; from: 0.9; to: 1.0; duration: Theme.animMed; easing.type: Easing.OutBack }
        }
    }
    exit: Transition {
        ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 1.0; to: 0.0; duration: Theme.animFast }
            NumberAnimation { property: "scale"; from: 1.0; to: 0.95; duration: Theme.animFast }
        }
    }

    contentItem: ColumnLayout {
        spacing: 12

        Text {
            text: "重新登入連結"
            color: Theme.text
            font.pixelSize: 17
            font.bold: true
            Layout.fillWidth: true
            Layout.topMargin: 20
            Layout.leftMargin: 22
            Layout.rightMargin: 22
        }
        Text {
            text: "把連結或 QR 給使用者；在目前網址開啟即可用原帳號登入並還原對話。"
            color: Theme.textDim
            font.pixelSize: 13
            lineHeight: 1.25
            wrapMode: Text.WordWrap
            Layout.fillWidth: true
            Layout.leftMargin: 22
            Layout.rightMargin: 22
        }

        // QR（給手機掃）
        Rectangle {
            Layout.alignment: Qt.AlignHCenter
            Layout.preferredWidth: 220
            Layout.preferredHeight: 220
            radius: Theme.radiusSmall
            color: Theme.surface
            border.width: 1
            border.color: Theme.surfaceBorder
            clip: true
            Image {
                id: reImg
                anchors.centerIn: parent
                width: 196
                height: 196
                fillMode: Image.PreserveAspectFit
                cache: false
                smooth: true
                asynchronous: true
                visible: source != "" && status === Image.Ready
            }
            Text {
                anchors.centerIn: parent
                visible: !reImg.visible
                text: "QR 產生失敗"
                color: Theme.textFaint
                font.pixelSize: 14
            }
        }

        // 連結（可全選複製）
        FieldInput {
            Layout.fillWidth: true
            Layout.leftMargin: 22
            Layout.rightMargin: 22
            readOnly: true
            text: backend.reloginUrl
        }
        Text {
            text: backend.reloginExpireText
            color: Theme.textDim
            font.pixelSize: 12
            Layout.fillWidth: true
            Layout.leftMargin: 22
            Layout.rightMargin: 22
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: 4
            Layout.bottomMargin: 18
            Layout.leftMargin: 22
            Layout.rightMargin: 22
            spacing: 10

            PrimaryButton {
                text: "複製連結"
                onClicked: backend.copyToClipboard(backend.reloginUrl)
            }
            GhostButton {
                text: "在此電腦開啟"
                onClicked: backend.openUrl(backend.reloginUrl)
            }
            Item { Layout.fillWidth: true }
            GhostButton {
                text: "關閉"
                onClicked: dlg.close()
            }
        }
    }
}
