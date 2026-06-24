const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/app.js"), "utf8");
const context = { escapeHTML: (value) => String(value), formatBytes: (value) => String(value) };
vm.runInNewContext(source.slice(source.indexOf("function renderTrafficChart("), source.indexOf("function renderDashboardNodes(")), context);
const items = Array.from({ length: 24 }, (_, i) => ({ total_bytes: i + 1, bucket_label: `${i}:00` }));

test("traffic chart uses measured width and every bucket boundary", () => {
  for (const width of [280, 1000, 1600]) {
    const svg = context.renderTrafficChart(items, width);
    assert.ok(svg.includes(`viewBox="0 0 ${width} 220"`));
    assert.equal((svg.match(/<line class="chart-grid"/g) || []).length, items.length + 1);
    assert.equal(svg.includes("<title>"), false);
    assert.equal(svg.includes("NaN"), false);
  }
});

test("mobile chart keeps all ticks but reduces labels", () => {
  const mobile = context.renderTrafficChart(items, 280);
  const desktop = context.renderTrafficChart(items, 1600);
  assert.ok((mobile.match(/<text/g) || []).length < (desktop.match(/<text/g) || []).length);
  assert.ok(context.renderTrafficChart([], 280).includes("Нет трафика за выбранный период."));
});
