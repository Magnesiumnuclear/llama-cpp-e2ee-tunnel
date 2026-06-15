import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 主視窗：漸層背景 + 服務列 + 分頁（滑動）+ 可調整的執行記錄。
// 所有業務邏輯都在後端 Controller，本檔僅負責呈現與互動。
ApplicationWindow {
    id: win
    width: 960
    height: 720
    minimumWidth: 780
    minimumHeight: 560
    visible: true
    title: "llama-proxy 控制面板"

    background: Rectangle {
        gradient: Gradient {
            GradientStop { position: 0.0; color: Theme.bgTop }
            GradientStop { position: 0.5; color: Theme.bgMid }
            GradientStop { position: 1.0; color: Theme.bgBottom }
        }
    }

    onClosing: backend.stopAll()

    Connections {
        target: backend
        function onWarningRaised(msg) { alertDialog.alert(msg); }
        function onLogAppended(text, kind) { logPanel.append(text, kind); }
        function onReloginReady() { reloginDialog.showResult(); }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 16
        spacing: 14

        // ── 服務列 ─────────────────────────────────────────
        ServiceBar { Layout.fillWidth: true }

        // ── 分頁 + 記錄（上下可調）──────────────────────────
        SplitView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            orientation: Qt.Vertical

            handle: Rectangle {
                implicitHeight: 12
                color: "transparent"
                Rectangle {
                    anchors.centerIn: parent
                    width: 48; height: 4; radius: 2
                    color: SplitHandle.pressed ? Theme.accent1
                         : (SplitHandle.hovered ? Qt.rgba(1, 1, 1, 0.4) : Qt.rgba(1, 1, 1, 0.16))
                    Behavior on color { ColorAnimation { duration: Theme.animFast } }
                }
            }

            // 上：分頁
            Item {
                SplitView.fillHeight: true
                SplitView.minimumHeight: 240

                ColumnLayout {
                    anchors.fill: parent
                    spacing: 12

                    TabBar {
                        id: tabBar
                        Layout.fillWidth: true
                        currentIndex: swipe.currentIndex
                        enabled: backend.proxyReady
                        spacing: 8
                        background: Rectangle { color: "transparent" }

                        NavTab { text: "QR 生成與預覽" }
                        NavTab { text: "權限審核" }
                        NavTab { text: "帳號總覽（預覽）" }
                    }

                    Item {
                        Layout.fillWidth: true
                        Layout.fillHeight: true

                        SwipeView {
                            id: swipe
                            anchors.fill: parent
                            clip: true
                            currentIndex: tabBar.currentIndex
                            enabled: backend.proxyReady
                            opacity: backend.proxyReady ? 1.0 : 0.35
                            Behavior on opacity { NumberAnimation { duration: Theme.animMed } }

                            QrView { }
                            PendingView { confirm: confirmDialog }
                            AccountsView { confirm: confirmDialog }
                        }

                        // 代理層就緒前的提示
                        Text {
                            anchors.centerIn: parent
                            visible: !backend.proxyReady
                            text: "啟動服務並待代理層就緒後即可使用"
                            color: Theme.textFaint
                            font.pixelSize: 15
                        }
                    }
                }
            }

            // 下：執行記錄
            LogPanel {
                id: logPanel
                SplitView.preferredHeight: 200
                SplitView.minimumHeight: 120
            }
        }
    }

    // 重複利用的對話框：確認（拒絕/刪除）與提醒（錯誤訊息）。
    ModalDialog { id: confirmDialog }
    ModalDialog { id: alertDialog }
    ReloginDialog { id: reloginDialog }
}
