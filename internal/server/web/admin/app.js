const state = {
  webPrefix: document.body.dataset.webPrefix || "/admin",
  csrfToken: "",
  accounts: [],
  nodes: [],
  deletedConnections: [],
  deletedConnectionsAccount: null,
  currentAccountID: "",
  currentNodeID: "",
  currentSummary: null,
  currentNode: null,
  dashboard: null,
  dashboardRange: "24h",
  showDeleted: false,
  provisionCopyValues: new Map(),
  provisionQRValues: new Map(),
  qrDialogValue: "",
  eventSource: null,
  reloadTimer: 0,
  activeInstallOperationID: "",
};

const app = document.querySelector("#app");
const toast = document.querySelector("#toast");
const logoutButton = document.querySelector("#logout");
const autofill = {
  accountEdited: false,
  userClientSlugEdited: false,
  clientSlugEdited: false,
};

const csrfKey = `laz_admin_csrf:${state.webPrefix}`;
state.csrfToken = localStorage.getItem(csrfKey) || "";
if (!state.csrfToken) {
  location.replace(`${state.webPrefix}/login`);
}

logoutButton.addEventListener("click", async () => {
  try {
    await api("/api/v1/auth/logout", { method: "POST" });
  } finally {
    localStorage.removeItem(csrfKey);
  }
  location.href = `${state.webPrefix}/login`;
});

document.addEventListener("click", (event) => {
  const link = event.target.closest("[data-link]");
  if (link) {
    event.preventDefault();
    history.pushState(null, "", link.getAttribute("href"));
    load();
    return;
  }
});

window.addEventListener("popstate", load);

startEvents();

function wireAutofill(form, { sourceName, targetName, editedFlag }) {
  const source = form.elements[sourceName];
  const target = form.elements[targetName];
  if (!source || !target) {
    return;
  }
  target.addEventListener("input", () => {
    autofill[editedFlag] = true;
  });
  source.addEventListener("input", () => {
    if (!autofill[editedFlag]) {
      target.value = window.lazUI.slugifyInput(source.value);
    }
  });
}

function resetAccountDialogDefaults(form) {
  autofill.accountEdited = false;
  autofill.userClientSlugEdited = false;
  form.elements.client_name.value = "Default";
  form.elements.client_slug.value = "default";
}

function renderAccountDialogBody() {
  return `
    <form id="account-form" method="dialog" class="laz-modal-fields">
      <label>
        Name
        <input name="display_name" required autocomplete="off">
      </label>
      <label>
        Account
        <input name="username" required autocomplete="off">
      </label>
      <div class="grid-two">
        <label>
          Client
          <input name="client_name" required value="Default" autocomplete="off">
        </label>
        <label>
          Client slug
          <input name="client_slug" required value="default" autocomplete="off">
        </label>
      </div>
      <fieldset>
        <legend>Nodes</legend>
        <div id="account-node-list" class="check-list"></div>
      </fieldset>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="submit" class="primary">Создать</button>
      </div>
    </form>
  `;
}

function openAccountDialog() {
  const dialog = window.lazUI.openModal({
    id: "account-dialog",
    title: "Добавить аккаунт",
    body: renderAccountDialogBody(),
  });
  const form = dialog.querySelector("#account-form");
  wireAutofill(form, {
    sourceName: "display_name",
    targetName: "username",
    editedFlag: "accountEdited",
  });
  wireAutofill(form, {
    sourceName: "client_name",
    targetName: "client_slug",
    editedFlag: "userClientSlugEdited",
  });
  resetAccountDialogDefaults(form);
  fillNodeChecks("account-node-list");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const nodeIDs = checkedNodeIDs("account-node-list");
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы одну ноду");
      return;
    }
    const username = String(data.get("username") || "").trim();
    const displayName = String(data.get("display_name") || "").trim();
    const enrollment = await api("/api/v1/enrollments", {
      method: "POST",
      body: {
        username,
        display_name: displayName,
        client: {
          slug: String(data.get("client_slug") || "").trim(),
          name: String(data.get("client_name") || "").trim(),
        },
        node_ids: nodeIDs,
      },
    });
    dialog.close();
    showToast("Аккаунт создан");
    await load();
    showProvisionResult(enrollment);
  });
}

async function load() {
  try {
    state.nodes = (await api("/api/v1/nodes")).items || [];
    state.showDeleted = isDeletedPage();
    const nodeID = currentNodeIDFromPath();
    if (nodeID) {
      state.currentNodeID = nodeID;
      state.currentNode = await api(`/api/v1/nodes/${encodeURIComponent(nodeID)}`);
      renderNodePage(state.currentNode);
      return;
    }
    state.currentNodeID = "";
    state.currentNode = null;
    const deletedConnectionsAccountID = deletedConnectionsAccountIDFromPath();
    if (deletedConnectionsAccountID) {
      state.currentAccountID = deletedConnectionsAccountID;
      state.currentSummary = null;
      const payload = await api(`/api/v1/accounts/${encodeURIComponent(deletedConnectionsAccountID)}/deleted-connections`);
      state.deletedConnections = payload.items || [];
      state.deletedConnectionsAccount = payload.account || null;
      renderDeletedConnectionsPage();
      return;
    }
    const accountID = currentAccountIDFromPath();
    state.currentAccountID = accountID;
    if (accountID) {
      state.currentSummary = await api(`/api/v1/accounts/${encodeURIComponent(accountID)}/summary`);
      renderAccountPage(state.currentSummary);
      return;
    }
    if (!isAccountsPage() && !state.showDeleted) {
      state.currentSummary = null;
      state.dashboard = await loadDashboard();
      renderDashboard(state.dashboard);
      return;
    }
    state.currentSummary = null;
    const accountsPath = state.showDeleted ? "/api/v1/accounts?include=summary&status=deleted" : "/api/v1/accounts?include=summary";
    state.accounts = (await api(accountsPath)).items || [];
    renderMain();
  } catch (error) {
    renderError(error);
  }
}

function renderError(error) {
  app.innerHTML = `
    <section class="empty">
      ${escapeHTML(error.message || String(error))}
    </section>
  `;
}

function startEvents() {
  if (state.eventSource) {
    state.eventSource.close();
  }
  const source = new EventSource("/api/v1/events");
  state.eventSource = source;
  source.onmessage = (event) => handleServerEvent(event);
  ["operation.created", "operation.step", "operation.failed", "node.created"].forEach((type) => {
    source.addEventListener(type, handleServerEvent);
  });
  source.onerror = () => {
    // EventSource reconnects automatically; keep the UI quiet unless a concrete event arrives.
  };
}

function handleServerEvent(event) {
  let payload = {};
  try {
    payload = JSON.parse(event.data || "{}");
  } catch (_) {
    return;
  }
  if (payload.message) {
    showToast(payload.message);
  }
  const detail = payload.payload || {};
  if (detail.operation?.id && detail.operation.id === state.activeInstallOperationID) {
    const form = document.querySelector("#hysteria-node-form");
    if (form) {
      renderInstallProgress(form, {
        operation: detail.operation,
        steps: detail.steps || [],
        logs: [payload.message || ""].filter(Boolean),
      });
    }
  }
  scheduleReloadForEvent(payload);
}

function scheduleReloadForEvent(event) {
  if (!["node", "account", "client", "connection", "operation"].includes(event.entity_type || "")) {
    return;
  }
  window.clearTimeout(state.reloadTimer);
  state.reloadTimer = window.setTimeout(() => {
    load();
  }, 400);
}

async function loadDashboard() {
  const params = dashboardRangeParams(state.dashboardRange);
  const search = new URLSearchParams({
    from_ms: String(params.fromMS),
    to_ms: String(params.toMS),
    bucket: "auto",
    limit: "10",
  });
  return api(`/api/v1/dashboard?${search.toString()}`);
}

function dashboardRangeParams(range) {
  const toMS = Date.now();
  const hours = range === "7d" ? 24 * 7 : range === "30d" ? 24 * 30 : 24;
  return { fromMS: toMS - hours * 60 * 60 * 1000, toMS };
}

function renderDashboard(data) {
  const summary = data.summary || {};
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Dashboard</h1>
        <p class="subline">Operational overview for selected range</p>
      </div>
      <div class="toolbar nav-toolbar">
        <select id="dashboard-range" class="range-select" aria-label="Dashboard range">
          <option value="24h"${state.dashboardRange === "24h" ? " selected" : ""}>Last 24 hours</option>
          <option value="7d"${state.dashboardRange === "7d" ? " selected" : ""}>Last 7 days</option>
          <option value="30d"${state.dashboardRange === "30d" ? " selected" : ""}>Last 30 days</option>
        </select>
        <button id="reload" type="button">Обновить</button>
      </div>
    </section>
    <section class="summary-grid">
      ${summaryCard("Online nodes", `${summary.online_nodes || 0} / ${summary.total_nodes || 0}`)}
      ${summaryCard("Online users", String(summary.online_users || 0))}
      ${summaryCard("Total traffic", formatBytes(summary.total_traffic_bytes || 0))}
      ${summaryCard("Availability", `${formatPercent(summary.availability_percent)}%`)}
      ${summary.offline_duration_ms ? summaryCard("Offline time", formatDuration(summary.offline_duration_ms)) : ""}
    </section>
    <section class="dashboard-card">
      <div class="card-head">
        <h2>Traffic over time</h2>
        <span class="muted">${escapeHTML((data.range || {}).bucket || "auto")} buckets</span>
      </div>
      ${renderTrafficChart(data.traffic_over_time || [])}
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2>Nodes overview</h2></div>
      ${renderDashboardNodes(data.nodes || [])}
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2>Online users</h2></div>
      ${renderDashboardOnlineUsers(data.online_users || [])}
    </section>
    <section class="ranking-grid">
      ${renderRankingCard("Top users by traffic", data.top_users_by_traffic || [], "display_name", "traffic_bytes")}
      ${renderRankingCard("Traffic by node", data.traffic_by_node || [], "name", "traffic_bytes")}
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2>Downtime</h2></div>
      ${renderDowntime(data.downtime || [])}
    </section>
  `;
  document.querySelector("#reload").addEventListener("click", load);
  document.querySelector("#dashboard-range").addEventListener("change", async (event) => {
    state.dashboardRange = event.target.value;
    await load();
  });
}

function summaryCard(label, value) {
  return `
    <article class="summary-card">
      <span>${escapeHTML(label)}</span>
      <strong>${escapeHTML(value)}</strong>
    </article>
  `;
}

function renderTrafficChart(items) {
  if (items.length === 0 || items.every((item) => !item.total_bytes)) {
    return `<section class="empty compact">No traffic in the selected range.</section>`;
  }
  const width = 720;
  const height = 180;
  const pad = 24;
  const maxValue = Math.max(...items.map((item) => item.total_bytes || 0), 1);
  const points = items.map((item, index) => {
    const x = pad + (items.length === 1 ? 0 : (index / (items.length - 1)) * (width - pad * 2));
    const y = height - pad - ((item.total_bytes || 0) / maxValue) * (height - pad * 2);
    return [x, y];
  });
  const line = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `${pad},${height - pad} ${line} ${width - pad},${height - pad}`;
  const labels = items.filter((_, i) => i === 0 || i === items.length - 1);
  return `
    <div class="traffic-chart">
      <svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Traffic over time">
        <polyline class="chart-grid" points="${pad},${pad} ${pad},${height - pad} ${width - pad},${height - pad}"></polyline>
        <polygon class="chart-area" points="${area}"></polygon>
        <polyline class="chart-line" points="${line}"></polyline>
        ${labels.map((item, index) => `
          <text x="${index === 0 ? pad : width - pad}" y="${height - 5}" text-anchor="${index === 0 ? "start" : "end"}">${escapeHTML(item.bucket_label || "")}</text>
        `).join("")}
        <text x="${width - pad}" y="${pad - 8}" text-anchor="end">${escapeHTML(formatBytes(maxValue))}</text>
      </svg>
    </div>
  `;
}

function renderDashboardNodes(nodes) {
  if (nodes.length === 0) {
    return `<section class="empty compact">No nodes connected yet.</section>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Node</th><th>Status</th><th>Hysteria2</th><th>Online</th><th>Traffic</th><th>Availability</th><th>Offline</th><th>Last heartbeat</th></tr></thead>
        <tbody>
          ${nodes.map((node) => `
            <tr>
              <td><a class="row-link" href="${state.webPrefix}/nodes/${encodeURIComponent(node.node_id)}" data-link>${escapeHTML(node.name || node.node_id)}</a></td>
              <td>${renderNodeStatus(node.status)}</td>
              <td>${escapeHTML(node.hysteria_status || "unknown")}</td>
              <td>${escapeHTML(String(node.online_users || 0))} users · ${escapeHTML(String(node.online_connections || 0))} conn</td>
              <td>${formatBytes(node.traffic_bytes || 0)}</td>
              <td>${formatPercent(node.availability_percent)}%</td>
              <td>${node.offline_duration_ms ? formatDuration(node.offline_duration_ms) : "—"}</td>
              <td>${relativeTime(node.last_heartbeat_ms)}</td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderDashboardOnlineUsers(items) {
  if (items.length === 0) {
    return `<section class="empty compact">No users are online right now.</section>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>User</th><th>Nodes</th><th>Connections</th><th>Traffic</th><th>Last seen</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td>${escapeHTML(item.display_name || item.credential_id)}<div class="muted mono">${escapeHTML(item.credential_id || "")}</div></td>
              <td>${escapeHTML((item.nodes || []).join(", "))}</td>
              <td>${escapeHTML(String(item.connections || 0))}</td>
              <td>${formatBytes(item.traffic_bytes || 0)}</td>
              <td>${relativeTime(item.last_seen_ms)}</td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderRankingCard(title, items, labelField, valueField) {
  const maxValue = Math.max(...items.map((item) => item[valueField] || 0), 1);
  return `
    <section class="dashboard-card ranking-card">
      <div class="card-head"><h2>${escapeHTML(title)}</h2></div>
      ${items.length === 0 ? `<section class="empty compact">No traffic in the selected range.</section>` : `
        <div class="ranking-list">
          ${items.map((item) => `
            <div class="ranking-row">
              <span class="rank-dot ${item.online ? "online" : ""}"></span>
              <span>${escapeHTML(item[labelField] || item.credential_id || item.node_id || "")}</span>
              <strong>${formatBytes(item[valueField] || 0)}</strong>
              <span class="rank-bar"><i style="width:${Math.max(4, ((item[valueField] || 0) / maxValue) * 100).toFixed(1)}%"></i></span>
            </div>
          `).join("")}
        </div>
      `}
    </section>
  `;
}

function renderDowntime(items) {
  if (items.length === 0) {
    return `<section class="empty compact">All nodes were online during the selected range.</section>`;
  }
  return `
    <div class="downtime-list">
      ${items.map((item) => `
        <div class="downtime-row">
          <strong>${escapeHTML(item.name || item.node_id)}</strong>
          <span>${formatDuration(item.offline_duration_ms || 0)} offline</span>
          <span class="muted">${item.currently_offline ? "currently offline" : "recovered"}</span>
        </div>
      `).join("")}
    </div>
  `;
}

function renderNodeStatus(status) {
  const value = status || "offline";
  const cls = value === "online" ? "active" : value === "degraded" ? "held" : "deleted";
  return `<span class="status ${cls}">${escapeHTML(value)}</span>`;
}

function renderMain() {
  const title = state.showDeleted ? "Удаленные пользователи" : "Аккаунты";
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">${title}</h1>
        <p class="subline">${state.accounts.length} ${plural(state.accounts.length, "запись", "записи", "записей")}</p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
        ${state.showDeleted
          ? `<a href="${state.webPrefix}/accounts" data-link><button type="button">К активным</button></a>`
          : `<a href="${state.webPrefix}/deleted" data-link><button type="button">Удаленные</button></a>`}
      </div>
    </section>
    <section class="toolbar action-toolbar">
      ${state.showDeleted ? "" : `
        <button id="open-account-dialog" type="button" class="primary">Добавить аккаунт</button>
        <button id="open-hysteria-node-dialog" type="button">Добавить Hysteria2 VPS</button>
        <button id="attach-hysteria-node-dialog" type="button">Attach Hysteria2 VPS</button>
      `}
    </section>
    ${renderAccountsTable(state.accounts)}
    <section class="nodes-footer">
      <strong>Ноды</strong>
      <div class="node-list">${renderNodePills(state.nodes)}</div>
    </section>
  `;
  document.querySelector("#reload").addEventListener("click", load);
  const openUserDialog = document.querySelector("#open-account-dialog");
  if (openUserDialog) {
    openUserDialog.addEventListener("click", openAccountDialog);
  }
  const openHysteriaNode = document.querySelector("#open-hysteria-node-dialog");
  if (openHysteriaNode) {
    openHysteriaNode.addEventListener("click", () => openHysteriaNodeDialog("install"));
  }
  const attachHysteriaNode = document.querySelector("#attach-hysteria-node-dialog");
  if (attachHysteriaNode) {
    attachHysteriaNode.addEventListener("click", () => openHysteriaNodeDialog("attach"));
  }
  bindMainActions();
}

function isAccountsPage() {
  return location.pathname === `${state.webPrefix}/accounts` || location.pathname === `${state.webPrefix}/accounts/`;
}

function renderHysteriaNodeDialogBody(mode = "install") {
  const isAttach = mode === "attach";
  return `
    <form id="hysteria-node-form" method="dialog" class="laz-modal-fields">
      <section class="notice warn">
        LazGate подключится к VPS по SSH и выполнит ${isAttach ? "attach commands для существующей Hysteria2" : "installation commands"}. Bootstrap SSH password используется только во время операции и не сохраняется.
      </section>
      ${isAttach ? `
        <section class="notice warn">
          Текущий /etc/hysteria/config.yaml будет сохранен рядом как lazgate-backup, после чего Hysteria2 будет переведена на LazGate auth и LazGate Agent.
        </section>
      ` : ""}
      <section class="notice">
        Проверьте firewall провайдера: TCP 80 inbound нужен для ACME HTTP challenge, UDP выбранного Hysteria2 порта нужен для клиентов.
      </section>
      <fieldset>
        <legend>SSH access</legend>
        <div class="grid-two">
          <label>
            VPS host/IP
            <input name="ssh_host" required autocomplete="off">
          </label>
          <label>
            SSH port
            <input name="ssh_port" type="number" min="1" max="65535" value="22" required>
          </label>
        </div>
        <div class="grid-two">
          <label>
            Bootstrap SSH user
            <input name="bootstrap_user" value="root" required autocomplete="off">
          </label>
          <label>
            Bootstrap SSH password
            <input name="bootstrap_password" type="password" required autocomplete="new-password">
          </label>
        </div>
      </fieldset>
      <fieldset>
        <legend>Node settings</legend>
        <label>
          Node name
          <input name="node_name" required autocomplete="off">
        </label>
        <div class="grid-two">
          <label>
            Public domain
            <input name="public_domain" autocomplete="off">
          </label>
          <label>
            Hysteria2 UDP port
            <input name="hysteria_port" type="number" min="1" max="65535" value="443" required>
          </label>
        </div>
        <label>
          Masquerade URL
          <input name="masquerade_url" value="https://news.ycombinator.com/" required autocomplete="off">
        </label>
      </fieldset>
      <details>
        <summary>Advanced settings</summary>
        <div class="laz-modal-fields">
          <label>
            Hysteria2 install version
            <input name="install_version" value="latest" autocomplete="off">
          </label>
          <label>
            ACME email
            <input name="acme_email" type="email" autocomplete="off">
            <span class="helper">Used only for TLS certificate issuance and renewal notifications from the certificate authority. If provided, this email is written into the Hysteria2 ACME configuration on your VPS. LazGate does not use this email for accounts, marketing, analytics, or any other purpose.</span>
          </label>
          <div class="grid-two">
            <label>
              Obfuscation type
              <input name="obfs_type" value="salamander" autocomplete="off">
            </label>
            <label>
              Traffic stats listen
              <input name="traffic_stats_listen" value="127.0.0.1:25413" autocomplete="off">
            </label>
          </div>
          <label class="checkbox-line">
            <input name="obfs_enabled" type="checkbox" checked>
            Obfuscation enabled
          </label>
          <label class="checkbox-line">
            <input name="traffic_stats_enabled" type="checkbox" checked>
            Traffic stats API enabled
          </label>
        </div>
      </details>
      <div id="hysteria-install-progress"></div>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="submit" class="primary">${isAttach ? "Attach и добавить ноду" : "Установить и добавить ноду"}</button>
      </div>
    </form>
  `;
}

function openHysteriaNodeDialog(mode = "install") {
  const isAttach = mode === "attach";
  const dialog = window.lazUI.openModal({
    id: "hysteria-node-dialog",
    title: isAttach ? "Attach Hysteria2 VPS" : "Add Hysteria2 VPS",
    body: renderHysteriaNodeDialogBody(mode),
  });
  const form = dialog.querySelector("#hysteria-node-form");
  const host = form.elements.ssh_host;
  const domain = form.elements.public_domain;
  let domainEdited = false;
  domain.addEventListener("input", () => {
    domainEdited = true;
  });
  host.addEventListener("input", () => {
    if (!domainEdited) {
      domain.value = generatedHysteriaDomain(host.value);
    }
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    renderInstallProgress(form, {
      steps: [
        "Connecting to VPS",
        "Checking system",
        "Creating lazgate SSH user",
        isAttach ? "Detecting existing Hysteria2" : "Installing Hysteria2",
        "Installing LazGate Agent",
        "Writing Hysteria2 config",
        "Starting Hysteria2 service",
        "Verifying service",
        "Registering node",
        "Waiting for LazGate Agent",
        "Done",
      ].map((name) => ({ name, status: "pending" })),
      logs: ["Installation request started. Keep this page open."],
    });
    try {
      const result = await api(isAttach ? "/api/v1/nodes/attach-hysteria2" : "/api/v1/nodes/install-hysteria2", {
        method: "POST",
        body: {
          ssh_host: String(data.get("ssh_host") || "").trim(),
          ssh_port: Number(data.get("ssh_port") || 22),
          bootstrap_user: String(data.get("bootstrap_user") || "root").trim(),
          bootstrap_password: String(data.get("bootstrap_password") || ""),
          node_name: String(data.get("node_name") || "").trim(),
          public_domain: String(data.get("public_domain") || "").trim(),
          hysteria_port: Number(data.get("hysteria_port") || 443),
          masquerade_url: String(data.get("masquerade_url") || "").trim(),
          install_version: String(data.get("install_version") || "latest").trim(),
          acme_email: String(data.get("acme_email") || "").trim(),
          obfs_enabled: Boolean(data.get("obfs_enabled")),
          obfs_type: String(data.get("obfs_type") || "salamander").trim(),
          traffic_stats_enabled: Boolean(data.get("traffic_stats_enabled")),
          traffic_stats_listen: String(data.get("traffic_stats_listen") || "127.0.0.1:25413").trim(),
          server_url: window.location.origin,
          agent_grpc_target: window.location.hostname ? `${window.location.hostname}:9443` : "",
        },
      });
      state.activeInstallOperationID = result.operation?.id || "";
      renderInstallProgress(form, result);
      showToast("Installation scheduled");
    } catch (error) {
      renderInstallProgress(form, error.payload || { logs: [error.message || String(error)] });
      showToast(error.message || "Installation failed");
    }
  });
}

function generatedHysteriaDomain(value) {
  const ip = String(value || "").trim();
  if (!/^(\d{1,3}\.){3}\d{1,3}$/.test(ip)) {
    return "";
  }
  return `h2.${ip.replaceAll(".", "-")}.sslip.io`;
}

function renderInstallProgress(form, result) {
  const target = form.querySelector("#hysteria-install-progress");
  if (!target) {
    return;
  }
  const steps = result.steps || [];
  const logs = result.logs || [];
  const operation = result.operation || {};
  target.innerHTML = `
    <section class="install-progress">
      <strong>Installation progress${operation.status ? ` · ${escapeHTML(operation.status)}` : ""}</strong>
      <div class="install-steps">
        ${steps.map((step) => `
          <div class="install-step ${escapeAttr(step.status || "pending")}">
            <span>${escapeHTML(step.name || "")}</span>
            <span>${escapeHTML(step.status || "pending")}</span>
          </div>
        `).join("")}
      </div>
      ${result.public_domain ? `<div class="subline">Domain: ${escapeHTML(result.public_domain)}</div>` : ""}
      ${logs.length > 0 ? `<pre class="install-log">${escapeHTML(logs.join("\\n"))}</pre>` : ""}
    </section>
  `;
}

function renderNodePage(details) {
  const node = details.node || {};
  const runtime = details.runtime || {};
  const totals = details.totals || {};
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Нода ${escapeHTML(node.name || node.id || "")}</h1>
        <p class="subline">
          ${escapeHTML(nodeTypeLabel(node.type))} · ${escapeHTML(node.region || "region not set")} · ${renderStatus(node.status || "active")}
        </p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
        <a href="${state.webPrefix}/accounts" data-link><button type="button">Назад</button></a>
      </div>
    </section>
    <section class="node-grid">
      <article class="node-card">
        <h2>Agent</h2>
        <dl class="node-facts">
          <div><dt>Status</dt><dd>${escapeHTML(runtime.agent_status || "unknown")}</dd></div>
          <div><dt>Heartbeat</dt><dd>${formatDateTime(runtime.last_heartbeat_at)}</dd></div>
          <div><dt>Version</dt><dd>${escapeHTML(runtime.agent_version || "-")}</dd></div>
          <div><dt>Protocol</dt><dd>${escapeHTML(runtime.protocol_version || "-")}</dd></div>
          <div><dt>Hysteria</dt><dd>${escapeHTML(runtime.hysteria_service_status || "-")}</dd></div>
          <div><dt>Queue</dt><dd>${escapeHTML(String(runtime.pending_usage_batch_count || 0))} batches · ${formatBytes(runtime.pending_usage_queue_size_bytes || 0)}</dd></div>
        </dl>
      </article>
      <article class="node-card">
        <h2>Traffic</h2>
        <dl class="node-facts">
          <div><dt>Online</dt><dd>${escapeHTML(String(totals.online_count || 0))}</dd></div>
          <div><dt>TX</dt><dd>${formatBytes(totals.tx_bytes || 0)}</dd></div>
          <div><dt>RX</dt><dd>${formatBytes(totals.rx_bytes || 0)}</dd></div>
          <div><dt>Total</dt><dd>${formatBytes(totals.total_bytes || 0)}</dd></div>
          <div><dt>Traffic sync</dt><dd>${formatDateTime(runtime.last_traffic_collection_at)}</dd></div>
          <div><dt>Online sync</dt><dd>${formatDateTime(runtime.last_online_collection_at)}</dd></div>
        </dl>
      </article>
    </section>
    <section class="toolbar action-toolbar">
      <button type="button" data-node-command="GetStatus">Get status</button>
      <button type="button" data-node-command="RestartHysteria">Restart Hysteria</button>
      <button type="button" data-node-command="StartHysteria">Start</button>
      <button type="button" data-node-command="StopHysteria" class="danger">Stop</button>
      <button type="button" data-node-command="CollectLogs">Collect logs</button>
      <button type="button" data-node-command="DumpStreams">Dump streams</button>
    </section>
    <section class="node-card">
      <h2>Online clients</h2>
      ${renderNodeOnline(details.online || [])}
    </section>
    <section class="node-card">
      <h2>Recent usage</h2>
      ${renderNodeUsage(details.usage || [])}
    </section>
    <section class="node-card">
      <h2>Commands</h2>
      ${renderNodeCommands(details.commands || [])}
    </section>
  `;
  document.querySelector("#reload").addEventListener("click", load);
  app.querySelectorAll("[data-node-command]").forEach((button) => {
    button.addEventListener("click", () => createNodeCommand(node.id, button.dataset.nodeCommand));
  });
}

function renderNodeOnline(items) {
  if (items.length === 0) {
    return `<div class="empty inline-empty">Нет online clients.</div>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Credential</th><th>Count</th><th>First seen</th><th>Last seen</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td class="mono">${escapeHTML(item.credential_id || "")}</td>
              <td>${escapeHTML(String(item.count || 0))}</td>
              <td>${formatDateTime(item.first_seen_at)}</td>
              <td>${formatDateTime(item.last_seen_at)}</td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderNodeUsage(items) {
  if (items.length === 0) {
    return `<div class="empty inline-empty">Traffic records пока нет.</div>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Credential</th><th>TX</th><th>RX</th><th>Total</th><th>Received</th></tr></thead>
        <tbody>
          ${items.slice(0, 50).map((item) => `
            <tr>
              <td class="mono">${escapeHTML(item.credential_id || "")}</td>
              <td>${formatBytes(item.tx_bytes || 0)}</td>
              <td>${formatBytes(item.rx_bytes || 0)}</td>
              <td>${formatBytes(item.total_bytes || 0)}</td>
              <td>${formatMSDate(item.received_at_ms)}</td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderNodeCommands(items) {
  if (items.length === 0) {
    return `<div class="empty inline-empty">Команд пока нет.</div>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Type</th><th>Status</th><th>Issued</th><th>Result</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td>${escapeHTML(item.Type || item.type || "")}</td>
              <td>${renderStatus(item.Status || item.status)}</td>
              <td>${formatDateTime(item.IssuedAt || item.issued_at)}</td>
              <td>
                <pre class="mini-log">${escapeHTML(item.Error || item.error || item.Result || item.result || "")}</pre>
              </td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
}

async function createNodeCommand(nodeID, type) {
  await api(`/api/v1/nodes/${encodeURIComponent(nodeID)}/commands`, {
    method: "POST",
    body: { type, payload: {}, expires_in_seconds: 300 },
  });
  showToast("Команда отправлена агенту");
  await load();
}

function renderDeletedConnectionsPage() {
  const account = state.deletedConnectionsAccount || {};
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Удаленные подключения</h1>
        <p class="subline">
          ${escapeHTML(account.display_name || account.username || account.id || "Аккаунт")} ·
          ${state.deletedConnections.length} ${plural(state.deletedConnections.length, "запись", "записи", "записей")}
        </p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
        <a href="${state.webPrefix}/accounts/${encodeURIComponent(account.id || state.currentAccountID)}" data-link><button type="button">К аккаунту</button></a>
      </div>
    </section>
    ${renderDeletedConnectionsTable(state.deletedConnections)}
  `;
  document.querySelector("#reload").addEventListener("click", load);
}

function renderDeletedConnectionsTable(items) {
  if (items.length === 0) {
    return `<section class="empty">Удаленных подключений нет.</section>`;
  }
  const rows = items.map((item) => {
    const connection = item.connection || {};
    const client = item.client || {};
    const node = item.node || {};
    return `
      <tr>
        <td data-label="Клиент">
          ${escapeHTML(client.name || client.slug || connection.client_id || "Client")}
          <div class="muted mono">${escapeHTML(client.slug || connection.client_id || "")}</div>
        </td>
        <td data-label="Нода">
          ${escapeHTML(node.name || connection.node_id || "Нода")}
          <div class="muted">${escapeHTML(nodeTypeLabel(node.type))}</div>
        </td>
        <td data-label="Тип">${protocolLabel(connection.protocol)}</td>
        <td data-label="Remote ID">
          ${escapeHTML(connection.remote_name || connection.remote_id || connection.id || "")}
          <div class="muted mono">${escapeHTML(connection.id || "")}</div>
        </td>
        <td data-label="Создано">${formatDateTime(connection.created_at)}</td>
        <td data-label="Удалено">${formatDateTime(connection.updated_at)}</td>
      </tr>
    `;
  }).join("");
  return `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Клиент</th>
            <th>Нода</th>
            <th>Тип</th>
            <th>Remote ID</th>
            <th>Создано</th>
            <th>Удалено</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function renderAccountsTable(accounts) {
  if (accounts.length === 0) {
    return `<section class="empty">${state.showDeleted ? "Удаленных аккаунтов нет." : "Аккаунтов пока нет."}</section>`;
  }
  const rows = accounts.map((summary) => {
    const account = summary.account;
    return `
      <tr>
        <td data-label="Имя">
          <a class="row-link" href="${state.webPrefix}/accounts/${encodeURIComponent(account.id)}" data-link>${escapeHTML(account.display_name || account.username)}</a>
          ${account.display_name ? `<div class="muted">account: ${escapeHTML(account.username)}</div>` : ""}
        </td>
        <td data-label="Клиенты">${renderClientSummary(summary.clients || [])}</td>
        <td data-label="Статус">${renderStatus(account.status)}</td>
        <td data-label="Создано">${formatDateTime(account.created_at)}</td>
        <td data-label="Действия">
          ${state.showDeleted ? `<span class="muted">Только просмотр</span>` : `<div class="inline-actions">
            <button type="button" data-copy-page="${escapeAttr(account.id)}">Страница конфигов</button>
            <button type="button" data-add-client="${escapeAttr(account.id)}">Добавить клиент</button>
          </div>`}
        </td>
      </tr>
    `;
  }).join("");
  return `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Имя</th>
            <th>Клиенты</th>
            <th>Статус</th>
            <th>Создано</th>
            <th>Действия</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function renderAccountPage(summary) {
  const account = summary.account;
  const isDeletedUser = account.status === "deleted";
  const titleName = account.display_name || account.username;
  const sublineParts = [];
  if (account.display_name) {
    sublineParts.push(`account: ${escapeHTML(account.username)}`);
  }
  sublineParts.push(renderStatus(account.status));
  if (account.created_at) {
    sublineParts.push(`создано: ${formatDateTime(account.created_at)}`);
  }
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">${isDeletedUser ? "Удаленный аккаунт" : "Аккаунт"} ${escapeHTML(titleName)}</h1>
        <p class="subline">
          ${sublineParts.join(" · ")}
        </p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
        <a href="${state.webPrefix}/" data-link><button type="button">Назад</button></a>
        <a href="${state.webPrefix}/accounts/${encodeURIComponent(account.id)}/deleted-connections" data-link><button type="button">Удаленные подключения</button></a>
      </div>
    </section>
    ${isDeletedUser ? "" : `
      <section class="toolbar action-toolbar">
        <button type="button" id="copy-account-page">Страница конфигов</button>
        <button type="button" id="show-account-page-qr">QR</button>
        <button id="open-client-dialog" type="button">Добавить клиент</button>
        <button id="open-connection-dialog" type="button">Добавить подключение</button>
        ${account.status === "held"
          ? `<button type="button" id="resume-account">Разблокировать</button>`
          : `<button type="button" id="hold-account">Заблокировать</button>`}
        <button type="button" id="delete-account" class="danger">Удалить</button>
      </section>
    `}
    ${renderAccessTable(summary)}
  `;
  bindAccountActions(summary);
  document.querySelector("#reload").addEventListener("click", load);
}

function renderAccessTable(summary) {
  const connections = summary.connections || [];
  const clients = summary.clients || [];
  const isDeletedUser = summary.account.status === "deleted";
  if (connections.length === 0 && clients.length === 0) {
    return `<section class="empty">Клиентов и подключений пока нет.</section>`;
  }
  const clientIDsWithConnection = new Set(connections.map((item) => item.connection.client_id));
  const connectionRows = connections.map((item) => {
    const connection = item.connection;
    return `
      <tr>
        <td data-label="Клиент">
          ${escapeHTML(item.client?.name || item.client?.slug || "Client")}
          <div class="muted mono">${escapeHTML(item.client?.slug || "")}</div>
        </td>
        <td data-label="Подключение">
          ${escapeHTML(connection.remote_name || connection.remote_id || connection.id)}
          <div class="muted">${escapeHTML(item.node?.name || connection.node_id)}</div>
        </td>
        <td data-label="Тип">${protocolLabel(connection.protocol)}</td>
        <td data-label="Статус">${renderStatus(connection.status)}</td>
        <td data-label="Создано">${formatDateTime(connection.created_at)}</td>
        ${isDeletedUser ? `
          <td data-label="Удалено">${connection.status === "deleted" ? formatDateTime(connection.updated_at) : `<span class="muted">-</span>`}</td>
          <td data-label="Действия">
            ${connection.status === "deleted"
              ? `<span class="muted">Удалено</span>`
              : `<button type="button" class="danger" data-delete-connection="${escapeAttr(connection.id)}">Удалить</button>`}
          </td>
        ` : `
          <td data-label="Действия">
            <div class="inline-actions">
              <button type="button" data-copy-client-page="${escapeAttr(connection.client_id)}">Страница конфигов</button>
              <button type="button" data-show-client-page-qr="${escapeAttr(connection.client_id)}">QR</button>
              ${connection.status === "held"
                ? `<button type="button" data-resume-connection="${escapeAttr(connection.id)}">Разблокировать</button>`
                : `<button type="button" data-hold-connection="${escapeAttr(connection.id)}">Заблокировать</button>`}
              <button type="button" class="danger" data-delete-connection="${escapeAttr(connection.id)}">Удалить</button>
            </div>
          </td>
        `}
      </tr>
    `;
  }).join("");
  const emptyClientRows = clients
    .filter((client) => !clientIDsWithConnection.has(client.id))
    .map((client) => `
      <tr>
        <td data-label="Клиент">
          ${escapeHTML(client.name || client.slug || "Client")}
          <div class="muted mono">${escapeHTML(client.slug || "")}</div>
        </td>
        <td data-label="Подключение"><span class="muted">Нет подключений</span></td>
        <td data-label="Тип"><span class="muted">-</span></td>
        <td data-label="Статус">${renderStatus(client.status)}</td>
        <td data-label="Создано">${formatDateTime(client.created_at)}</td>
        ${isDeletedUser ? `
          <td data-label="Удалено">${client.status === "deleted" ? formatDateTime(client.updated_at) : `<span class="muted">-</span>`}</td>
          <td data-label="Действия"><span class="muted">Только просмотр</span></td>
        ` : `
          <td data-label="Действия">
            <div class="inline-actions">
              <button type="button" data-copy-client-page="${escapeAttr(client.id)}">Страница конфигов</button>
              <button type="button" data-show-client-page-qr="${escapeAttr(client.id)}">QR</button>
              <button type="button" data-add-connection-client="${escapeAttr(client.id)}">Добавить подключение</button>
            </div>
          </td>
        `}
      </tr>
    `).join("");
  const rows = connectionRows + emptyClientRows;
  return `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Клиент</th>
            <th>Подключение</th>
            <th>Тип</th>
            <th>Статус</th>
            <th>Создано</th>
            ${isDeletedUser ? `
              <th>Удалено</th>
              <th>Действия</th>
            ` : `
              <th>Действия</th>
            `}
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function bindMainActions() {
  app.querySelectorAll("[data-copy-page]").forEach((button) => {
    button.addEventListener("click", () => copyAccountTokenURL(button.dataset.copyPage, "config_page"));
  });
  app.querySelectorAll("[data-add-client]").forEach((button) => {
    button.addEventListener("click", async () => {
      history.pushState(null, "", `${state.webPrefix}/accounts/${encodeURIComponent(button.dataset.addClient)}`);
      await load();
      openClientDialog();
    });
  });
}

function bindAccountActions(summary) {
  const copyPage = document.querySelector("#copy-account-page");
  if (copyPage) {
    copyPage.addEventListener("click", () => copyAccountTokenURL(summary.account.id, "config_page"));
  }
  const showPageQR = document.querySelector("#show-account-page-qr");
  if (showPageQR) {
    showPageQR.addEventListener("click", () => showConfigPageQR(summary.account.id));
  }
  app.querySelectorAll("[data-copy-client-page]").forEach((button) => {
    button.addEventListener("click", () => copyAccountTokenURL(summary.account.id, "config_page", button.dataset.copyClientPage));
  });
  app.querySelectorAll("[data-show-client-page-qr]").forEach((button) => {
    button.addEventListener("click", () => showConfigPageQR(summary.account.id, button.dataset.showClientPageQr));
  });
  const openclient = document.querySelector("#open-client-dialog");
  if (openclient) {
    openclient.addEventListener("click", openClientDialog);
  }
  const openAccess = document.querySelector("#open-connection-dialog");
  if (openAccess) {
    openAccess.addEventListener("click", openConnectionDialog);
  }
  const hold = document.querySelector("#hold-account");
  if (hold) {
    hold.addEventListener("click", () => accountAction(summary.account.id, "hold"));
  }
  const resume = document.querySelector("#resume-account");
  if (resume) {
    resume.addEventListener("click", () => accountAction(summary.account.id, "resume"));
  }
  const deleteUser = document.querySelector("#delete-account");
  if (deleteUser) {
    deleteUser.addEventListener("click", () => {
      openConfirmDialog({
        title: "Удалить аккаунт?",
        message: "Будут удалены аккаунт и все его подключения.",
        confirmLabel: "Удалить",
        onConfirm: async () => {
          await accountAction(summary.account.id, "delete");
        },
      });
    });
  }
  app.querySelectorAll("[data-hold-connection]").forEach((button) => {
    button.addEventListener("click", () => connectionAction(button.dataset.holdConnection, "hold"));
  });
  app.querySelectorAll("[data-resume-connection]").forEach((button) => {
    button.addEventListener("click", () => connectionAction(button.dataset.resumeConnection, "resume"));
  });
  app.querySelectorAll("[data-delete-connection]").forEach((button) => {
    button.addEventListener("click", () => {
      openConfirmDialog({
        title: "Удалить подключение?",
        message: "Подключение будет удалено на всех связанных нодах.",
        confirmLabel: "Удалить",
        onConfirm: async () => {
          await connectionAction(button.dataset.deleteConnection, "delete");
        },
      });
    });
  });
  app.querySelectorAll("[data-add-connection-client]").forEach((button) => {
    button.addEventListener("click", () => openConnectionDialog(button.dataset.addConnectionClient));
  });
}

function openConfirmDialog({ title, message, confirmLabel = "Подтвердить", onConfirm }) {
  const dialog = window.lazUI.openModal({
    id: "confirm-dialog",
    title,
    compact: true,
    body: `
      <p class="subline">${escapeHTML(message || "")}</p>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="button" class="danger" id="confirm-action">${escapeHTML(confirmLabel)}</button>
      </div>
    `,
  });
  dialog.querySelector("#confirm-action").addEventListener("click", async () => {
    try {
      await onConfirm?.();
    } finally {
      dialog.close();
    }
  });
}

function showProvisionResult(enrollment) {
  const dialog = window.lazUI.openModal({
    id: "provision-dialog",
    title: "Аккаунт создан",
    wide: true,
    body: `<div id="provision-result" class="provision-result"></div>`,
    actions: `<button type="button" data-close-dialog>Закрыть</button>`,
  });
  const provisionResult = dialog.querySelector("#provision-result");
  state.provisionCopyValues = new Map();
  state.provisionQRValues = new Map();
  provisionResult.innerHTML = renderProvisionResult(enrollment);
  provisionResult.querySelectorAll("[data-copy-provision]").forEach((button) => {
    button.addEventListener("click", () => copyText(state.provisionCopyValues.get(button.dataset.copyProvision) || ""));
  });
  renderProvisionQRCodes(provisionResult);
}

function renderProvisionResult(enrollment) {
  const account = enrollment.account || {};
  const client = enrollment.client || {};
  const links = [
    ["config_page", "Страница конфигов", enrollment.config_page],
  ].filter(([, , value]) => value);
  const errors = (enrollment.results || []).filter((item) => item.status === "error");
  return `
    <section class="provision-summary">
      <div>
        <strong>${escapeHTML(account.display_name || account.username || "Аккаунт")}</strong>
        <div class="muted">${escapeHTML(account.username || "")} · ${escapeHTML(client.name || client.slug || "Client")}</div>
      </div>
      ${renderStatus(account.status || "active")}
    </section>
    <section class="provision-section">
      <h3>Ссылки</h3>
      <div class="provision-list">
        ${links.map(([key, label, value]) => renderProvisionValue(key, label, value)).join("")}
      </div>
    </section>
    ${errors.length === 0 ? "" : `
      <section class="provision-section">
        <h3>Ошибки</h3>
        <div class="provision-list">
          ${errors.map((item) => `
            <div class="provision-item error">
              <div>
                <strong>${escapeHTML(item.node?.name || item.node?.id || "Нода")}</strong>
                <div class="muted">${escapeHTML(item.error || "Provisioning error")}</div>
              </div>
            </div>
          `).join("")}
        </div>
      </section>
    `}
  `;
}

function renderProvisionValue(key, label, value) {
  state.provisionCopyValues.set(key, value);
  return `
    <div class="provision-item">
      <div>
        <strong>${escapeHTML(label)}</strong>
        <div class="mono value-line">${escapeHTML(value)}</div>
      </div>
      <button type="button" data-copy-provision="${escapeAttr(key)}">Скопировать</button>
    </div>
    ${renderProvisionQR(key, value)}
  `;
}

function renderProvisionQR(key, value) {
  if (!value) {
    return "";
  }
  state.provisionQRValues.set(key, value);
  return `<div class="qr-box" data-qr-provision="${escapeAttr(key)}"><span class="muted">QR...</span></div>`;
}

async function renderProvisionQRCodes(provisionResult) {
  for (const [key, value] of state.provisionQRValues.entries()) {
    const target = provisionResult.querySelector(`[data-qr-provision="${cssEscape(key)}"]`);
    if (!target) {
      continue;
    }
    await renderQRImage(target, "/api/v1/qr", { value }, csrfHeaders());
  }
}

async function copyAccountTokenURL(accountID, field, clientID = "") {
  await copyText(await accountTokenURL(accountID, field, clientID));
}

async function accountTokenURL(accountID, field, clientID = "") {
  const payload = await api("/api/v1/tokens", {
    method: "POST",
    body: { account_id: accountID, client_id: clientID || "" },
  });
  return payload[field] || "";
}

async function showConfigPageQR(accountID, clientID = "") {
  state.qrDialogValue = await accountTokenURL(accountID, "config_page", clientID);
  const dialog = window.lazUI.openModal({
    id: "qr-dialog",
    title: "QR страницы конфигов",
    body: `
      <div id="qr-dialog-target" class="qr-dialog-target"></div>
      <div id="qr-dialog-value" class="mono value-line"></div>
    `,
    actions: `
      <button type="button" id="copy-qr-dialog-value">Скопировать ссылку</button>
      <button type="button" data-close-dialog>Закрыть</button>
    `,
  });
  const qrDialogValue = dialog.querySelector("#qr-dialog-value");
  const qrDialogTarget = dialog.querySelector("#qr-dialog-target");
  dialog.querySelector("#copy-qr-dialog-value").addEventListener("click", () => copyText(state.qrDialogValue));
  qrDialogValue.textContent = state.qrDialogValue;
  qrDialogTarget.className = "qr-box qr-dialog-target";
  qrDialogTarget.innerHTML = `<span class="muted">QR...</span>`;
  await renderQRImage(qrDialogTarget, "/api/v1/qr", { value: state.qrDialogValue }, csrfHeaders());
}

async function accountAction(accountID, action) {
  await api(`/api/v1/accounts/${encodeURIComponent(accountID)}/${action}`, { method: "POST" });
  showToast("Готово");
  await load();
}

async function connectionAction(connectionID, action) {
  await api(`/api/v1/connections/${encodeURIComponent(connectionID)}/${action}`, { method: "POST" });
  showToast("Готово");
  await load();
}

async function provisionSelectedNodes(clientID, nodeIDs) {
  for (const nodeID of nodeIDs) {
    const node = state.nodes.find((item) => item.id === nodeID);
    await api("/api/v1/connections/provision", {
      method: "POST",
      body: {
        account_id: state.currentSummary.account.id,
        client_id: clientID,
        node_id: nodeID,
        protocol: protocolForNode(node),
      },
    });
  }
}

function openClientDialog() {
  if (!state.currentSummary) {
    return;
  }
  const next = (state.currentSummary.clients || []).length + 1;
  autofill.clientSlugEdited = false;
  const dialog = window.lazUI.openModal({
    id: "client-dialog",
    title: "Добавить клиент",
    body: `
      <form id="client-form" method="dialog" class="laz-modal-fields">
        <div class="grid-two">
          <label>
            Client
            <input name="client_name" required autocomplete="off">
          </label>
          <label>
            Client slug
            <input name="client_slug" required autocomplete="off">
          </label>
        </div>
        <fieldset>
          <legend>Nodes</legend>
          <div id="client-node-list" class="check-list"></div>
        </fieldset>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="primary">Добавить клиент</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#client-form");
  wireAutofill(form, {
    sourceName: "client_name",
    targetName: "client_slug",
    editedFlag: "clientSlugEdited",
  });
  form.elements.client_name.value = `Client ${next}`;
  form.elements.client_slug.value = window.lazUI.slugifyInput(form.elements.client_name.value);
  fillNodeChecks("client-node-list");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const nodeIDs = checkedNodeIDs("client-node-list");
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы одну ноду");
      return;
    }
    const slug = String(data.get("client_slug") || "").trim();
    const existingClient = findClientBySlug(slug);
    let client = existingClient;
    let provisionNodeIDs = nodeIDs;
    if (client) {
      const usedNodeIDs = activeNodeIDsForClient(client.id);
      provisionNodeIDs = nodeIDs.filter((nodeID) => !usedNodeIDs.has(nodeID));
      if (provisionNodeIDs.length === 0) {
        showToast("Клиент уже существует и выбранные nodes уже подключены");
        return;
      }
    } else {
      client = await api("/api/v1/clients", {
        method: "POST",
        body: {
          account_id: state.currentSummary.account.id,
          slug,
          name: String(data.get("client_name") || "").trim(),
        },
      });
    }
    await provisionSelectedNodes(client.id, provisionNodeIDs);
    dialog.close();
    autofill.clientSlugEdited = false;
    showToast(existingClient ? "Подключение добавлено к существующему клиенту" : "Клиент добавлен");
    await load();
  });
}

function openConnectionDialog(clientID = "") {
  if (!state.currentSummary) {
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "connection-dialog",
    title: "Добавить подключение",
    body: `
      <form id="connection-form" method="dialog" class="laz-modal-fields">
        <label>
          Клиент
          <select name="client_id" required></select>
        </label>
        <fieldset>
          <legend>Nodes</legend>
          <div id="connection-node-list" class="check-list"></div>
        </fieldset>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="primary">Добавить подключение</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#connection-form");
  fillClientOptions(form);
  const hasClient = [...form.elements.client_id.options].some((option) => option.value === clientID);
  if (clientID && hasClient) {
    form.elements.client_id.value = clientID;
  }
  fillConnectionNodeChecks(form);
  form.elements.client_id.addEventListener("change", () => fillConnectionNodeChecks(form));
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const selectedClientID = String(data.get("client_id") || "").trim();
    const nodeIDs = checkedNodeIDs("connection-node-list");
    if (!selectedClientID) {
      showToast("Выберите клиент");
      return;
    }
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы одну ноду");
      return;
    }
    await provisionSelectedNodes(selectedClientID, nodeIDs);
    dialog.close();
    showToast("Подключение добавлено");
    await load();
  });
}

function findClientBySlug(slug) {
  return (state.currentSummary?.clients || []).find((client) => client.slug === slug) || null;
}

function fillClientOptions(form) {
  const select = form.elements.client_id;
  const clients = state.currentSummary.clients || [];
  if (clients.length === 0) {
    select.innerHTML = `<option value="">Нет устройств</option>`;
    select.disabled = true;
    return;
  }
  select.disabled = false;
  select.innerHTML = clients.map((client) => `
    <option value="${escapeAttr(client.id)}">
      ${escapeHTML(client.name || client.slug || "Client")}
    </option>
  `).join("");
}

function fillConnectionNodeChecks(form) {
  const clientID = String(form.elements.client_id.value || "");
  const usedNodeIDs = activeNodeIDsForClient(clientID);
  fillNodeChecks("connection-node-list", usedNodeIDs);
}

function activeNodeIDsForClient(clientID) {
  const used = new Set();
  for (const item of state.currentSummary?.connections || []) {
    const connection = item.connection;
    if (connection.client_id === clientID && connection.status !== "deleted") {
      used.add(connection.node_id);
    }
  }
  return used;
}

function fillNodeChecks(targetID, excludeNodeIDs = new Set()) {
  const target = document.querySelector(`#${targetID}`);
  const supported = state.nodes.filter((node) => protocolForNode(node) && !excludeNodeIDs.has(node.id));
  if (supported.length === 0) {
    target.innerHTML = excludeNodeIDs.size > 0
      ? `<div class="muted">Для выбранного клиента уже есть подключения ко всем доступным nodes.</div>`
      : `<div class="muted">Нет поддерживаемых active nodes.</div>`;
    return;
  }
  target.innerHTML = supported.map((node) => `
    <label class="check-row">
      <input type="checkbox" value="${escapeAttr(node.id)}" checked>
      <span>${escapeHTML(node.name)} <span class="muted">(${nodeTypeLabel(node.type)})</span></span>
    </label>
  `).join("");
}

function checkedNodeIDs(targetID) {
  return [...document.querySelectorAll(`#${targetID} input[type="checkbox"]:checked`)].map((item) => item.value);
}

async function api(path, options = {}) {
  const init = {
    method: options.method || "GET",
    credentials: "same-origin",
    headers: {
      ...csrfHeaders(),
    },
  };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  const response = await fetch(path, init);
  const contentType = response.headers.get("content-type") || "";
  const data = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = typeof data === "object" && data.error ? data.error : `HTTP ${response.status}`;
    if (response.status === 401) {
      localStorage.removeItem(csrfKey);
      location.replace(`${state.webPrefix}/login`);
      return null;
    }
    const error = new Error(message);
    error.payload = data;
    throw error;
  }
  return data;
}

function csrfHeaders() {
  return state.csrfToken ? { "X-CSRF-Token": state.csrfToken } : {};
}

async function copyText(value) {
  if (!value) {
    showToast("Нечего копировать");
    return;
  }
  await navigator.clipboard.writeText(value);
  showToast("Скопировано");
}

function currentAccountIDFromPath() {
  const escaped = state.webPrefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = location.pathname.match(new RegExp(`^${escaped}/accounts/([^/]+)$`));
  return match ? decodeURIComponent(match[1]) : "";
}

function currentNodeIDFromPath() {
  const escaped = state.webPrefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = location.pathname.match(new RegExp(`^${escaped}/nodes/([^/]+)$`));
  return match ? decodeURIComponent(match[1]) : "";
}

function isDeletedPage() {
  return location.pathname === `${state.webPrefix}/deleted`;
}

function deletedConnectionsAccountIDFromPath() {
  const escaped = state.webPrefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = location.pathname.match(new RegExp(`^${escaped}/accounts/([^/]+)/deleted-connections$`));
  return match ? decodeURIComponent(match[1]) : "";
}

function findDeletedConfig(id) {
  for (const item of state.deletedConnections || []) {
    for (const cfg of item.configs || []) {
      if (cfg.id === id) {
        return cfg;
      }
    }
  }
  return null;
}

function renderClientSummary(clients) {
  if (clients.length === 0) {
    return `<span class="muted">Нет устройств</span>`;
  }
  const names = clients.slice(0, 3).map((client) => escapeHTML(client.name || client.slug));
  const more = clients.length > 3 ? ` ... (${clients.length})` : "";
  return `${names.join(", ")}${more}`;
}

function renderNodePills(nodes) {
  if (nodes.length === 0) {
    return `<span class="muted">Нет nodes</span>`;
  }
  return nodes.map((node) => `
    <a class="node-pill" href="${state.webPrefix}/nodes/${encodeURIComponent(node.id)}" data-link>
      <span>${escapeHTML(node.name)}</span>
      <span class="muted">(${nodeTypeLabel(node.type)})</span>
      <span class="status ${escapeAttr(node.agent_status === "online" ? "active" : "held")}">${escapeHTML(node.agent_status || "offline")}</span>
      <span class="muted">${escapeHTML(node.hysteria_service_status || "-")}</span>
      <span class="muted">${escapeHTML(String(node.online_count || 0))} online</span>
    </a>
  `).join("");
}

function renderStatus(status) {
  const value = status || "unknown";
  return `<span class="status ${escapeAttr(value)}">${statusLabel(value)}</span>`;
}

function nodeTypeLabel(type) {
  switch (type) {
    case "amnezia_api":
      return "AmneziaWG";
    case "blitz_hysteria":
      return "Hysteria2";
    case "native_hysteria":
      return "Hysteria2";
    default:
      return type || "Unknown";
  }
}

function protocolForNode(node) {
  if (!node) {
    return "";
  }
  switch (node.type) {
    case "amnezia_api":
      return "amneziawg";
    case "blitz_hysteria":
      return "hysteria2";
    case "native_hysteria":
      return "hysteria2";
    default:
      return "";
  }
}

function protocolLabel(protocol) {
  switch (protocol) {
    case "amneziawg":
      return "AmneziaWG";
    case "hysteria2":
      return "Hysteria2";
    default:
      return escapeHTML(protocol || "Unknown");
  }
}

function statusLabel(status) {
  switch (status) {
    case "active":
      return "Активно";
    case "held":
      return "Заблокировано";
    case "deleted":
      return "Удалено";
    case "error":
      return "Ошибка";
    default:
      return status;
  }
}

function configLabel(config) {
  const client = config.client ? `${config.client} · ` : "";
  return `${client}${config.kind}`;
}

function formatDateTime(value) {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return escapeHTML(value);
  }
  return escapeHTML(date.toLocaleString());
}

function formatMSDate(value) {
  const ms = Number(value || 0);
  if (!ms) {
    return "";
  }
  return formatDateTime(new Date(ms).toISOString());
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = bytes;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatPercent(value) {
  const num = Number(value || 0);
  return Number.isFinite(num) ? num.toFixed(num % 1 === 0 ? 0 : 1) : "0";
}

function formatDuration(ms) {
  let seconds = Math.floor(Number(ms || 0) / 1000);
  if (seconds <= 0) {
    return "0s";
  }
  const days = Math.floor(seconds / 86400);
  seconds %= 86400;
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  seconds %= 60;
  const parts = [];
  if (days) parts.push(`${days}d`);
  if (hours) parts.push(`${hours}h`);
  if (minutes && parts.length < 2) parts.push(`${minutes}m`);
  if (!parts.length) parts.push(`${seconds}s`);
  return parts.join(" ");
}

function relativeTime(ms) {
  const value = Number(ms || 0);
  if (!value) {
    return "—";
  }
  const diff = Date.now() - value;
  if (diff < 45_000) {
    return "now";
  }
  if (diff < 3_600_000) {
    return `${Math.round(diff / 60_000)}m ago`;
  }
  if (diff < 86_400_000) {
    return `${Math.round(diff / 3_600_000)}h ago`;
  }
  return `${Math.round(diff / 86_400_000)}d ago`;
}

function isAmneziaConfig(config) {
  return config.client === "amnezia" || String(config.kind || "").startsWith("amnezia");
}

async function renderQRImage(target, path, body, headers = {}) {
  try {
    const response = await fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        ...headers,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      target.innerHTML = `<span class="muted">${escapeHTML(error.error || `QR error ${response.status}`)}</span>`;
      return;
    }
    const contentType = response.headers.get("content-type") || "";
    if (contentType.includes("application/json")) {
      const data = await response.json();
      const items = data.items || [];
      target.classList.toggle("qr-series", items.length > 1);
      target.innerHTML = items.map((src, index) => `
        <figure>
          <img src="${escapeAttr(src)}" alt="QR code ${index + 1}">
          ${items.length > 1 ? `<figcaption>${index + 1}/${items.length}</figcaption>` : ""}
        </figure>
      `).join("");
      return;
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    target.classList.remove("qr-series");
    target.innerHTML = `<img src="${escapeAttr(url)}" alt="QR code">`;
  } catch (error) {
    target.innerHTML = `<span class="muted">${escapeHTML(error.message || String(error))}</span>`;
  }
}

function plural(count, one, few, many) {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (mod10 === 1 && mod100 !== 11) {
    return one;
  }
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) {
    return few;
  }
  return many;
}

function showToast(message) {
  window.lazUI.showToast(toast, message);
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttr(value) {
  return escapeHTML(value);
}

function cssEscape(value) {
  if (window.CSS && CSS.escape) {
    return CSS.escape(value);
  }
  return String(value).replaceAll('"', '\\"');
}

load();
