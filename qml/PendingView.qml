import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 分頁二：權限審核（待審清單 + 核准/拒絕 + L1/L2/L3）。
Item {
    id: page
    property var confirm: null   // 由 Main 注入的確認對話框

    GlassCard {
        anchors.fill: parent

        ColumnLayout {
            anchors.fill: parent
            spacing: 12

            RowLayout {
                Layout.fillWidth: true
                spacing: 12
                GhostButton { text: "重新整理"; onClicked: backend.refreshPending() }
                Text {
                    text: backend.pendingCountText
                    color: Theme.textDim
                    font.pixelSize: 14
                }
                Item { Layout.fillWidth: true }
            }

            ListView {
                id: list
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 10
                model: pendingModel
                ScrollBar.vertical: ScrollBar { }

                populate: Transition {
                    NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.animMed }
                    NumberAnimation { property: "x"; from: 36; to: 0; duration: Theme.animMed; easing.type: Easing.OutCubic }
                }
                add: Transition {
                    NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.animMed }
                    NumberAnimation { property: "x"; from: 36; to: 0; duration: Theme.animMed; easing.type: Easing.OutCubic }
                }
                displaced: Transition {
                    NumberAnimation { properties: "x,y"; duration: Theme.animMed; easing.type: Easing.OutCubic }
                }

                delegate: Rectangle {
                    width: ListView.view.width
                    height: 66
                    radius: Theme.radiusSmall
                    color: Theme.surface
                    border.width: 1
                    border.color: Theme.surfaceBorder

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 14
                        anchors.rightMargin: 14
                        spacing: 12

                        ColumnLayout {
                            spacing: 3
                            Text {
                                text: accountId
                                color: Theme.text
                                font.pixelSize: 14
                                font.bold: true
                            }
                            Text {
                                text: deviceId + "　·　" + createdAt
                                color: Theme.textFaint
                                font.pixelSize: 12
                            }
                        }
                        Item { Layout.fillWidth: true }

                        ComboField {
                            id: permCombo
                            model: ["L1", "L2", "L3"]
                            currentIndex: {
                                var i = ["L1", "L2", "L3"].indexOf(permission);
                                return i < 0 ? 1 : i;
                            }
                        }
                        PrimaryButton {
                            text: "核准"
                            onClicked: backend.approve(accountId, permCombo.currentText)
                        }
                        GhostButton {
                            text: "拒絕"
                            danger: true
                            onClicked: page.confirm.ask(
                                "確認",
                                "確定拒絕 " + accountId + "？",
                                function () { backend.reject(accountId); })
                        }
                    }
                }
            }
        }
    }
}
