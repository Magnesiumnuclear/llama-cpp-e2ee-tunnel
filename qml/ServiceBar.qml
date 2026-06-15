import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 服務列：全部啟動／停止、Tunnel 與代理層狀態、公網 URL。
// 高度依內容自動決定（不撐滿，故內層以 width 綁定、不使用 fillHeight）。
GlassCard {
    id: bar

    ColumnLayout {
        width: parent.width
        spacing: 14

        // 啟停 + 狀態
        RowLayout {
            Layout.fillWidth: true
            spacing: 12

            PrimaryButton { text: "▶ 全部啟動"; onClicked: backend.startAll() }
            GhostButton { text: "■ 停止全部"; onClicked: backend.stopAll() }

            Item { Layout.preferredWidth: 8 }

            Text { text: "Tunnel"; color: Theme.textDim; font.pixelSize: 13 }
            StatusPill { label: backend.tunnelStatusText; kind: backend.tunnelStatusKind }

            Item { Layout.preferredWidth: 6 }

            Text { text: "代理層"; color: Theme.textDim; font.pixelSize: 13 }
            StatusPill { label: backend.proxyStatusText; kind: backend.proxyStatusKind }

            // 編譯狀態：沿用上次編譯 / 編譯中 / 已重新編譯
            Rectangle {
                visible: backend.buildModeText !== ""
                Layout.preferredHeight: 22
                Layout.preferredWidth: buildModeLabel.implicitWidth + 18
                radius: 11
                color: Qt.rgba(1, 1, 1, 0.06)
                border.width: 1
                border.color: Theme.surfaceBorder
                Text {
                    id: buildModeLabel
                    anchors.centerIn: parent
                    text: backend.buildModeText
                    color: Theme.textDim
                    font.pixelSize: 12
                }
            }

            Item { Layout.fillWidth: true }
        }

        // 公網 URL
        RowLayout {
            Layout.fillWidth: true
            spacing: 10

            Text { text: "公網 URL"; color: Theme.textDim; font.pixelSize: 13 }
            FieldInput {
                Layout.fillWidth: true
                readOnly: true
                text: backend.publicUrl
                placeholderText: "啟動後自動填入…"
            }
            GhostButton {
                text: "複製"
                onClicked: backend.copyToClipboard(backend.publicUrl)
            }
        }
    }
}
