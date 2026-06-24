const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/app.js"), "utf8");
const context = { escapeHTML: String, formatPercent: (value) => value.toFixed(1) };
vm.runInNewContext(source.slice(source.indexOf("function serverHealthCard("), source.indexOf("function renderTrafficChart(")), context);

test("server health distinguishes empty, healthy, partial and total outage", () => {
  assert.match(context.serverHealthCard({}), /Серверы не добавлены/);
  assert.match(context.serverHealthCard({ total_nodes: 3, online_nodes: 3 }), /summary-good/);
  const partial = context.serverHealthCard({ total_nodes: 3, online_nodes: 2 });
  assert.match(partial, /summary-warning/);
  assert.match(partial, /Недоступно серверов: 1/);
  assert.match(partial, /href="#dashboard-servers"/);
  const outage = context.serverHealthCard({ total_nodes: 3, online_nodes: 0 });
  assert.match(outage, /summary-danger/);
  assert.match(outage, /Все серверы недоступны/);
});

test("availability reflects the selected period and does not round downtime to healthy", () => {
  assert.match(context.availabilityCard({ total_nodes: 0 }), /Нет данных/);
  for (const [value, tone] of [[100, "good"], [99.999, "warning"], [99, "warning"], [98.9, "danger"], [0, "danger"]]) {
    const card = context.availabilityCard({ total_nodes: 1, availability_percent: value });
    assert.ok(card.includes(`summary-${tone}`));
    if (value < 100) assert.ok(card.includes("Были простои"));
  }
  assert.match(context.availabilityCard({ total_nodes: 1, availability_percent: 99.999 }), /< 100%/);
});
