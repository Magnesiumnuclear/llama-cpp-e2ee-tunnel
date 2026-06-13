import QtQuick
import QtQuick.Controls.Basic

// 統一風格的下拉選單（L1/L2/L3 等）。
ComboBox {
    id: cb
    implicitHeight: 36
    implicitWidth: 84
    font.pixelSize: 13

    contentItem: Text {
        leftPadding: 12
        rightPadding: 26
        text: cb.displayText
        color: Theme.text
        font: cb.font
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
    }

    background: Rectangle {
        radius: Theme.radiusSmall
        color: Theme.surface
        border.width: 1
        border.color: (cb.activeFocus || cb.hovered) ? Theme.accent1 : Theme.surfaceBorder
        Behavior on border.color { ColorAnimation { duration: Theme.animFast } }
    }

    indicator: Canvas {
        x: cb.width - width - 11
        y: (cb.height - height) / 2
        width: 10; height: 6
        onPaint: {
            var ctx = getContext("2d");
            ctx.reset();
            ctx.moveTo(0, 0);
            ctx.lineTo(width, 0);
            ctx.lineTo(width / 2, height);
            ctx.closePath();
            ctx.fillStyle = Qt.rgba(Theme.text.r, Theme.text.g, Theme.text.b, 0.75);
            ctx.fill();
        }
    }

    delegate: ItemDelegate {
        width: cb.width
        height: 32
        highlighted: cb.highlightedIndex === index
        contentItem: Text {
            text: modelData
            color: Theme.text
            font: cb.font
            leftPadding: 10
            verticalAlignment: Text.AlignVCenter
        }
        background: Rectangle {
            radius: 6
            color: highlighted ? Qt.rgba(Theme.accent1.r, Theme.accent1.g, Theme.accent1.b, 0.25)
                               : "transparent"
        }
    }

    popup: Popup {
        y: cb.height + 4
        width: cb.width
        implicitHeight: Math.min(contentItem.implicitHeight + 8, 220)
        padding: 4
        background: Rectangle {
            radius: Theme.radiusSmall
            color: "#222a52"
            border.width: 1
            border.color: Theme.cardBorder
        }
        contentItem: ListView {
            clip: true
            implicitHeight: contentHeight
            model: cb.popup.visible ? cb.delegateModel : null
            currentIndex: cb.highlightedIndex
            ScrollIndicator.vertical: ScrollIndicator { }
        }
        enter: Transition {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.animFast }
        }
    }
}
