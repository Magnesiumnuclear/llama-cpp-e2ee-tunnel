import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts
import App.Icons 1.0

// 執行記錄面板：每行依「種類(kind)」畫一個向量嚴重度圖示並上色，新行淡入、自動捲到底（上限 2000 行）。
GlassCard {
    id: panel

    function append(text, kind) {
        logModel.append({ "text": text, "kind": kind || "info" });
        if (logModel.count > 2000)
            logModel.remove(0, logModel.count - 2000);
        listView.positionViewAtEnd();
    }

    function kindColor(kind) {
        if (kind === "success") return Theme.ok;
        if (kind === "error") return Theme.bad;
        if (kind === "warn") return Theme.idle;
        if (kind === "action") return Theme.accent1;
        return Theme.textDim;
    }

    function kindIcon(kind) {
        if (kind === "success") return "check";
        if (kind === "error") return "cross";
        if (kind === "warn") return "warning";
        if (kind === "action") return "key";
        return "dot";
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 10

        SectionHeader { title: "執行記錄" }

        ListView {
            id: listView
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            model: ListModel { id: logModel }
            ScrollBar.vertical: ScrollBar { }

            add: Transition {
                NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.animMed }
            }

            delegate: RowLayout {
                width: ListView.view.width
                spacing: 7
                VectorIcon {
                    Layout.preferredWidth: 13
                    Layout.preferredHeight: 13
                    Layout.alignment: Qt.AlignTop
                    Layout.topMargin: 2
                    name: panel.kindIcon(model.kind)
                    color: panel.kindColor(model.kind)
                }
                Text {
                    Layout.fillWidth: true
                    text: model.text
                    color: panel.kindColor(model.kind)
                    font.family: "Consolas"
                    font.pixelSize: 12
                    textFormat: Text.PlainText
                    wrapMode: Text.WrapAnywhere
                }
            }
        }
    }
}
