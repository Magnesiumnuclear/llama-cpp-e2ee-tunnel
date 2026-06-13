# -*- coding: utf-8 -*-
"""QML 用的清單資料模型。

以單一泛用 `DictListModel` 取代舊版的 QTableWidget；每一列就是一個 dict，
role 名稱直接對應 QML delegate 內可用的屬性名，達到「資料與介面分離」。
"""

from PyQt6.QtCore import QAbstractListModel, QModelIndex, QByteArray, Qt


class DictListModel(QAbstractListModel):
    """以 dict 為單位的通用 list model；role 名稱即 dict 的鍵。

    QML 端：`ListView { model: pendingModel; delegate: ... accountId, deviceId ... }`
    """

    def __init__(self, role_names, parent=None):
        super().__init__(parent)
        self._rows = []
        base = int(Qt.ItemDataRole.UserRole) + 1
        self._roles = {base + i: QByteArray(name.encode())
                       for i, name in enumerate(role_names)}
        self._idx2name = {base + i: name for i, name in enumerate(role_names)}

    # ── QAbstractListModel 介面 ────────────────────────────────
    def rowCount(self, parent=QModelIndex()):
        return 0 if parent.isValid() else len(self._rows)

    def data(self, index, role=Qt.ItemDataRole.DisplayRole):
        if not index.isValid():
            return None
        name = self._idx2name.get(role)
        if name is None:
            return None
        return self._rows[index.row()].get(name, "")

    def roleNames(self):
        return self._roles

    # ── 資料更新 ──────────────────────────────────────────────
    def set_rows(self, rows):
        """整批替換內容。rows 為已正規化、鍵對齊 role 名稱的 dict 串列。"""
        self.beginResetModel()
        self._rows = list(rows)
        self.endResetModel()

    def clear(self):
        self.set_rows([])
