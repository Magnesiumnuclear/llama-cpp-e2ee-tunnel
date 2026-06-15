import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts
import App.Icons 1.0

// 分頁三：帳號總覽（已通過 / 未通過），含 E2E 與刪除操作。
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
                GhostButton { text: "重新整理"; onClicked: backend.refreshAccounts() }
                Text {
                    text: backend.accountsCountText
                    color: Theme.textDim
                    font.pixelSize: 13
                    Layout.fillWidth: true
                    elide: Text.ElideRight
                }
            }

            ListView {
                id: list
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 10
                model: accountsModel
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
                    height: 72
                    radius: Theme.radiusSmall
                    color: Theme.surface
                    border.width: 1
                    border.color: Theme.surfaceBorder

                    // 左側狀態色條
                    Rectangle {
                        anchors.left: parent.left
                        anchors.top: parent.top
                        anchors.bottom: parent.bottom
                        anchors.margins: 1
                        width: 4
                        radius: 2
                        color: statusColor
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 16
                        anchors.rightMargin: 14
                        spacing: 12

                        ColumnLayout {
                            spacing: 3
                            RowLayout {
                                spacing: 8
                                Text {
                                    text: accountId
                                    color: Theme.text
                                    font.pixelSize: 14
                                    font.bold: true
                                }
                                Rectangle {
                                    radius: 9
                                    height: 20
                                    width: stRow.implicitWidth + 16
                                    color: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.18)
                                    border.width: 1
                                    border.color: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.5)
                                    RowLayout {
                                        id: stRow
                                        anchors.centerIn: parent
                                        spacing: 5
                                        VectorIcon {
                                            Layout.preferredWidth: 13
                                            Layout.preferredHeight: 13
                                            Layout.alignment: Qt.AlignVCenter
                                            name: status === "active" ? "check_circle"
                                                : status === "pending_approval" ? "hourglass"
                                                : status === "rejected" ? "cross_circle"
                                                : status === "disabled" ? "ban" : "dot"
                                            color: statusColor
                                        }
                                        Text {
                                            text: statusView
                                            color: statusColor
                                            font.pixelSize: 11
                                            font.bold: true
                                            Layout.alignment: Qt.AlignVCenter
                                        }
                                    }
                                }
                            }
                            Text {
                                text: "裝置 " + deviceId + "　·　權限 " + (permission || "—")
                                      + "　·　建立 " + createdAt
                                      + (approvedAt ? "　·　核准 " + approvedAt : "")
                                color: Theme.textFaint
                                font.pixelSize: 12
                                elide: Text.ElideRight
                                Layout.maximumWidth: 520
                            }
                        }
                        Item { Layout.fillWidth: true }

                        GhostButton {
                            iconName: "relogin"
                            text: "重新登入"
                            visible: isActive
                            onClicked: backend.reloginAccount(accountId)
                        }
                        GhostButton {
                            iconName: "lock"
                            text: "E2E"
                            visible: isActive
                            onClicked: backend.openE2eTest(accountId)
                        }
                        GhostButton {
                            iconName: "ban"
                            text: "撤銷登入"
                            visible: isActive
                            onClicked: page.confirm.ask(
                                "確認撤銷登入",
                                "撤銷帳號「" + accountId + "」目前所有 token？\n所有裝置會立即登出，使用者需重新登入（用於憑證外洩時止血）。",
                                function () { backend.revokeSessions(accountId); })
                        }
                        GhostButton {
                            iconName: "trash"
                            text: "刪除"
                            danger: true
                            onClicked: page.confirm.ask(
                                "確認刪除",
                                "確定刪除帳號「" + accountId + "」？\n此操作將同時移除所有 session，無法還原。",
                                function () { backend.deleteAccount(accountId); })
                        }
                    }
                }
            }
        }
    }
}
