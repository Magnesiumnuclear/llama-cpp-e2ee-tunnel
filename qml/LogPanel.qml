import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Layouts

// 執行記錄面板：等寬字、依前綴上色、新行淡入、自動捲到底（上限 2000 行）。
GlassCard {
    id: panel

    function append(line) {
        logModel.append({ "line": line });
        if (logModel.count > 2000)
            logModel.remove(0, logModel.count - 2000);
        listView.positionViewAtEnd();
    }

    function lineColor(s) {
        if (s.indexOf("✓") === 0 || s.indexOf("✅") === 0) return Theme.ok;
        if (s.indexOf("✗") === 0 || s.indexOf("❌") === 0) return Theme.bad;
        if (s.indexOf("⚠") === 0) return Theme.idle;
        if (s.indexOf("🔑") === 0 || s.indexOf("🔐") === 0 || s.indexOf("🗑") === 0) return Theme.accent1;
        if (s.indexOf("[tunnel]") === 0) return "#7fd1c9";
        if (s.indexOf("[proxy]") === 0) return "#b6a6f0";
        return Theme.textDim;
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

            delegate: Text {
                width: ListView.view.width
                text: line
                color: panel.lineColor(line)
                font.family: "Consolas"
                font.pixelSize: 12
                textFormat: Text.PlainText
                wrapMode: Text.WrapAnywhere
            }
        }
    }
}
