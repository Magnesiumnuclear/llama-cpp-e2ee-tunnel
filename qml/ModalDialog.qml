import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 可重複利用的強制回應對話框，支援兩種用法：
//   ask(title, message, callback) → 確認/取消（用於拒絕、刪除等危險動作）
//   alert(message)                → 單一「知道了」（用於提醒/錯誤訊息）
Popup {
    id: dlg
    property string heading: ""
    property string message: ""
    property string confirmText: "確定"
    property string cancelText: "取消"
    property bool showCancel: true
    property color confirmColor1: Theme.accent1
    property color confirmColor2: Theme.accent2
    property var onConfirm: null

    function ask(t, m, cb) {
        heading = t; message = m; onConfirm = cb;
        showCancel = true; confirmText = "確定"; cancelText = "取消";
        confirmColor1 = Theme.danger1; confirmColor2 = Theme.danger2;
        open();
    }
    function alert(m) {
        heading = "提醒"; message = m; onConfirm = null;
        showCancel = false; confirmText = "知道了";
        confirmColor1 = Theme.accent1; confirmColor2 = Theme.accent2;
        open();
    }

    modal: true
    dim: true
    width: 400
    anchors.centerIn: Overlay.overlay
    padding: 0
    closePolicy: Popup.CloseOnEscape

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
        spacing: 14

        Text {
            text: dlg.heading
            color: Theme.text
            font.pixelSize: 17
            font.bold: true
            Layout.fillWidth: true
            Layout.topMargin: 20
            Layout.leftMargin: 22
            Layout.rightMargin: 22
        }
        Text {
            text: dlg.message
            color: Theme.textDim
            font.pixelSize: 14
            lineHeight: 1.25
            wrapMode: Text.WordWrap
            Layout.fillWidth: true
            Layout.leftMargin: 22
            Layout.rightMargin: 22
        }
        RowLayout {
            Layout.alignment: Qt.AlignRight
            Layout.topMargin: 4
            Layout.bottomMargin: 18
            Layout.rightMargin: 22
            spacing: 10

            GhostButton {
                text: dlg.cancelText
                visible: dlg.showCancel
                onClicked: dlg.close()
            }
            PrimaryButton {
                text: dlg.confirmText
                c1: dlg.confirmColor1
                c2: dlg.confirmColor2
                onClicked: {
                    if (dlg.onConfirm) dlg.onConfirm();
                    dlg.close();
                }
            }
        }
    }
}
