/* Shared current-state presentation for node installation dialogs. */
window.LazInstallProgress = class {
  constructor(target, steps, webPrefix) {
    this.target = target;
    this.steps = steps.map((step) => ({ ...step, status: "pending" }));
    this.webPrefix = webPrefix;
    this.operationID = "";
    this.status = "running";
    this.message = "";
    this.node = null;
    this.buffer = [];
    this.render();
  }

  accept(result) {
    this.operationID = result.operation?.id || result.operation_id || "";
    if (result.steps?.length && typeof result.steps[0] === "object") {
      this.steps = result.steps.map((step) => ({ ...step, status: this.normalize(step.status) }));
    }
    this.status = "running";
    for (const event of this.buffer) this.apply(event);
    this.buffer = [];
    this.render();
  }

  event(event) {
    if (!this.operationID) {
      if (this.buffer.length < 100) this.buffer.push(event);
      return;
    }
    this.apply(event);
    this.render();
  }

  normalize(status) {
    return ["ok", "applied", "done"].includes(status) ? "ok" : (status || "pending");
  }

  apply(event) {
    const detail = event.payload || {};
    if (detail.operation_id !== this.operationID || this.status === "done") return;
    if (detail.status === "scheduled") return;
    const step = this.steps.find((item) => item.type === detail.step_type);
    if (step) step.status = this.normalize(detail.status);
    if (detail.status === "done") {
      this.status = "done";
      this.node = detail.node || null;
      this.message = "";
      this.steps.forEach((item) => { item.status = "ok"; });
      return;
    }
    this.status = detail.status === "failed" ? "failed" : "running";
    this.message = this.status === "failed"
      ? [detail.message, detail.error_id].filter(Boolean).join(" · ")
      : "";
  }

  fail(message) {
    this.status = "failed";
    this.message = message;
    this.buffer = [];
    this.render();
  }

  render() {
    if (!this.target) return;
    const doc = this.target.ownerDocument;
    const element = (tag, text, className) => {
      const node = doc.createElement(tag);
      if (text) node.textContent = text;
      if (className) node.className = className;
      return node;
    };
    const labels = { pending: "Ожидает", running: "Выполняется", ok: "Готово", failed: "Ошибка" };
    const titles = { running: "Установка выполняется", failed: "Ошибка установки", done: "Установка завершена" };
    const section = element("section", "", "install-progress");
    section.append(element("strong", titles[this.status]));
    const list = element("div", "", "install-steps");
    const active = this.status === "running" ? this.steps.findIndex((step) => step.status !== "ok") : -1;
    this.steps.forEach((step, index) => {
      const status = index === active ? "running" : step.status;
      const row = element("div", "", "install-step " + status);
      row.append(element("span", step.name), element("span", labels[status] || status));
      list.append(row);
    });
    section.append(list);
    if (this.message) section.append(element("p", this.message, "install-error"));
    if (this.status === "done" && this.node?.id) {
      const link = element("a", this.node.name || "Открыть сервер");
      link.href = this.webPrefix + "/nodes/" + encodeURIComponent(this.node.id);
      section.append(link);
    }
    this.target.replaceChildren(section);
  }
};
