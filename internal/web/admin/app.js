const state = {
  webPrefix: document.body.dataset.webPrefix || "/admin",
  csrfToken: "",
  accounts: [],
  nodes: [],
  deletedConnections: [],
  deletedConnectionsAccount: null,
  currentAccountID: "",
  currentSummary: null,
  showDeleted: false,
  provisionCopyValues: new Map(),
  provisionQRValues: new Map(),
  qrDialogValue: "",
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
          ? `<a href="${state.webPrefix}/" data-link><button type="button">К активным</button></a>`
          : `<a href="${state.webPrefix}/deleted" data-link><button type="button">Удаленные</button></a>`}
      </div>
    </section>
    <section class="toolbar action-toolbar">
      ${state.showDeleted ? "" : `<button id="open-account-dialog" type="button" class="primary">Добавить аккаунт</button>`}
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
  bindMainActions();
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
    throw new Error(message);
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
    <span class="node-pill">${escapeHTML(node.name)} <span class="muted">(${nodeTypeLabel(node.type)})</span></span>
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
      return "Native Hysteria";
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
