const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");

const context = { window: {} };
vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/install-progress.js"), "utf8"), context);
const Progress = context.window.LazInstallProgress;

for (const protocol of ["hysteria", "awg"]) {
  const event = (status, step = "prepare", extra = {}) => ({
    payload: { operation_id: "one", step_type: protocol + ".install." + step, status, ...extra },
  });
  const model = () => new Progress(null, ["prepare", "poll", "done"].map((step) => ({
    type: protocol + ".install." + step, name: step,
  })), "/admin");

  test(protocol + ": retry updates one row and clears previous error", () => {
    const progress = model();
    progress.accept({ operation_id: "one" });
    progress.event(event("failed", "poll", { message: "INSTALLER_FAILED" }));
    assert.equal(progress.status, "failed");
    progress.event(event("applied", "poll"));
    assert.equal(progress.status, "running");
    assert.equal(progress.message, "");
    assert.equal(progress.steps.length, 3);
    assert.equal(progress.steps[1].status, "ok");
  });

  test(protocol + ": final status and node survive stale events", () => {
    const progress = model();
    progress.accept({ operation: { id: "one" } });
    progress.event(event("done", "done", { node: { id: "node", name: "Server" } }));
    progress.event(event("scheduled"));
    progress.event(event("failed", "poll"));
    assert.equal(progress.status, "done");
    assert.equal(progress.node.id, "node");
    assert.ok(progress.steps.every((step) => step.status === "ok"));
  });

  test(protocol + ": SSE before POST response is buffered and unrelated operations ignored", () => {
    const progress = model();
    progress.event({ payload: { operation_id: "other", status: "done" } });
    progress.event(event("applied"));
    progress.accept({ operation_id: "one", steps: [
      { type: protocol + ".install.prepare", name: "Prepare", status: "pending" },
    ] });
    assert.equal(progress.steps[0].status, "ok");
    assert.equal(progress.status, "running");
    assert.equal(progress.buffer.length, 0);
  });
}
