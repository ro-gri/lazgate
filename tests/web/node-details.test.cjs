const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/app.js"), "utf8");
const context = {
  escapeHTML: (v) => String(v).replaceAll("<", "&lt;"), formatBytes: String,
  formatMSDate: String, formatDateTime: String, renderStatus: String,
  NATIVE_AMNEZIAWG_NODE_TYPE: "native_amneziawg",
};
vm.runInNewContext(source.slice(source.indexOf("function nodeRuntimeStatusLabel("), source.indexOf("async function createNodeCommand(")), context);

test("node usage shows account, connection name, ID and grouped totals", () => {
  const html = context.renderNodeUsage([{account_name: "Alice", connection_name: "Phone", connection_id: "con-1", tx_bytes: 10, rx_bytes: 20, total_bytes: 30, from_ms: 100, to_ms: 200}]);
  for (const value of ["Пользователь", "Подключение", "ConnectionID", "Alice", "Phone", "con-1", "100 - 200"]) assert.ok(html.includes(value));
  assert.equal((html.match(/<tbody>/g) || []).length, 1);
});

test("node actions and states use Russian labels", () => {
  assert.equal(context.nodeRuntimeStatusLabel("running"), "Активен");
  assert.equal(context.nodeRuntimeStatusLabel("active"), "Активен");
  assert.equal(context.nodeRuntimeStatusLabel("online"), "На связи");
  for (const type of ["native_amneziawg", "native_hysteria"]) {
    const html = context.renderNodeRuntimeActions({type});
    assert.ok(html.includes(">Перезапустить</button>"));
    assert.ok(html.includes("Проверить состояние"));
  }
  assert.equal(context.nodeCommandLabel("RestartAmneziaWG"), "Перезапустить");
});

test("runtime commands have their own status labels", () => {
  for (const [status, label] of [["active", "Ожидает результата"], ["completed", "Завершена"], ["error", "Ошибка"]]) {
    assert.ok(context.renderCommandStatus(status).includes(label));
    assert.ok(context.renderNodeCommands([{type: "GetStatus", status}]).includes(label));
  }
});
