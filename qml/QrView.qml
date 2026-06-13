import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 分頁一：QR 生成與預覽。
Item {
    id: page

    GlassCard {
        anchors.fill: parent

        ColumnLayout {
            anchors.fill: parent
            spacing: 14

            // account_id 輸入列
            RowLayout {
                Layout.fillWidth: true
                spacing: 10
                Text { text: "account_id"; color: Theme.textDim; font.pixelSize: 13 }
                FieldInput {
                    id: accField
                    Layout.fillWidth: true
                    placeholderText: "留空＝自動產生新帳號；或自訂名稱（如 使用者A手機、user_phone_002）"
                    onAccepted: backend.generateQr(text)
                }
                PrimaryButton {
                    text: "生成 QR"
                    onClicked: backend.generateQr(accField.text)
                }
            }

            // QR 預覽區：吃掉剩餘高度，但允許壓縮至 0，確保「註冊網址」永遠留在卡片內。
            Rectangle {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.minimumHeight: 0
                Layout.preferredHeight: 320
                radius: Theme.radiusSmall
                color: Theme.surface
                border.width: 1
                border.color: Theme.surfaceBorder
                clip: true

                Image {
                    id: qrImg
                    anchors.centerIn: parent
                    width: Math.max(0, Math.min(parent.width - 36, parent.height - 36, 320))
                    height: width
                    fillMode: Image.PreserveAspectFit
                    cache: false
                    smooth: true
                    asynchronous: true
                    visible: source != "" && status === Image.Ready
                }
                Text {
                    anchors.centerIn: parent
                    visible: !qrImg.visible
                    text: backend.qrPlaceholder
                    color: Theme.textFaint
                    font.pixelSize: 15
                }

                NumberAnimation {
                    id: qrPop
                    target: qrImg
                    property: "scale"
                    from: 0.85; to: 1.0
                    duration: Theme.animMed
                    easing.type: Easing.OutBack
                }
                Connections {
                    target: backend
                    // 每次生成都強制清空再設定來源，確保「同名檔案」也會重新載入。
                    function onQrUpdated() {
                        qrImg.source = "";
                        qrImg.source = backend.qrImageUrl;
                        if (backend.qrImageUrl !== "")
                            qrPop.restart();
                    }
                }
            }

            // 註冊網址列
            RowLayout {
                Layout.fillWidth: true
                spacing: 10
                Text { text: "註冊網址"; color: Theme.textDim; font.pixelSize: 13 }
                FieldInput {
                    Layout.fillWidth: true
                    readOnly: true
                    text: backend.qrRegisterUrl
                }
                GhostButton {
                    text: "在瀏覽器開啟"
                    onClicked: backend.openUrl(backend.qrRegisterUrl)
                }
            }

            Text {
                Layout.fillWidth: true
                text: backend.qrExpireText
                color: Theme.textDim
                font.pixelSize: 13
            }
        }
    }
}
