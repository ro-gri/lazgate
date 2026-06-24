const MAX_EVENT_HISTORY = 100;
const AMNEZIA_NODE_TYPE = "amnezia_api";
const NATIVE_AMNEZIAWG_NODE_TYPE = "native_amneziawg";
let dashboardChartObserver;

const state = {
  webPrefix: document.body.dataset.webPrefix || "/admin",
  csrfToken: "",
  accounts: [],
  nodes: [],
  admins: [],
  adminInvites: [],
  currentPrincipal: null,
  adminAccessForbidden: false,
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
  eventHistory: [],
  reloadTimer: 0,
  refreshTimer: 0,
  refreshAfterMS: 0,
  installProgress: null,
};

const DEFAULT_ADMIN_INVITE_STATUS_FILTER = "active";
const ADMIN_INVITE_FILTERS = [
  { value: "active", label: "Активные" },
  { value: "used", label: "Использованные" },
  { value: "expired", label: "Истекшие" },
  { value: "revoked", label: "Отозванные" },
  { value: "all", label: "Все" },
];

const app = document.querySelector("#app");
const toast = document.querySelector("#toast");
const eventHistoryLink = document.querySelector("#event-history-link");
const eventHistoryCount = document.querySelector("#event-history-count");
const logoutButton = document.querySelector("#logout");
const adminNavLinks = document.querySelectorAll("[data-admin-section]");
const privilegedAdminLinks = document.querySelectorAll("[data-privileged-admin]");
const autofill = {
  accountEdited: false,
};

const apiBase = `${state.webPrefix}/api/v1`;
const csrfKey = `laz_admin_csrf:${state.webPrefix}`;
const refreshAfterKey = `laz_admin_refresh_after:${state.webPrefix}`;
state.csrfToken = localStorage.getItem(csrfKey) || "";
state.refreshAfterMS = Date.parse(localStorage.getItem(refreshAfterKey) || "") || 0;
if (!state.csrfToken) {
  location.replace(`${state.webPrefix}/login`);
}

if (eventHistoryLink) {
  eventHistoryLink.addEventListener("click", openEventHistory);
}

logoutButton.addEventListener("click", async () => {
  try {
    await api("/api/v1/auth/logout", { method: "POST" });
  } finally {
    clearAdminAuthStorage();
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
document.addEventListener("visibilitychange", handleVisibilityChange);
window.addEventListener("pagehide", () => {
  stopRefreshTimer();
  stopEvents();
});
window.addEventListener("pageshow", handleVisiblePage);

handleVisiblePage();

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
  form.elements.connection_name.value = "Default";
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
      <label>
        Подключение
        <input name="connection_name" required value="Default" autocomplete="off">
      </label>
      <fieldset>
        <legend>Серверы</legend>
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
  resetAccountDialogDefaults(form);
  fillNodeChecks("account-node-list");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const nodeIDs = checkedNodeIDs("account-node-list");
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы один сервер");
      return;
    }
    const username = String(data.get("username") || "").trim();
    const displayName = String(data.get("display_name") || "").trim();
    const enrollment = await api("/api/v1/enrollments", {
      method: "POST",
      body: {
        username,
        display_name: displayName,
        connection_name: String(data.get("connection_name") || "").trim(),
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
  dashboardChartObserver?.disconnect();
  dashboardChartObserver = null;
  try {
    await ensureCurrentPrincipal();
    updatePrivilegedAdminNavigation();
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
    if (isNodesPage()) {
      state.currentSummary = null;
      renderNodesPage();
      return;
    }
    const administrationSection = currentAdministrationSection();
    if (administrationSection) {
      state.currentSummary = null;
      if (!canUseAdministration()) {
        renderAdminsForbiddenPage();
        return;
      }
      await loadAdminsPage(administrationSection);
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

async function ensureCurrentPrincipal() {
  if (state.currentPrincipal) {
    return state.currentPrincipal;
  }
  const me = await api("/api/v1/auth/me");
  state.currentPrincipal = me?.principal || null;
  return state.currentPrincipal;
}

function canUseAdministration() {
  return ["owner", "admin"].includes(state.currentPrincipal?.role || "");
}

function updatePrivilegedAdminNavigation() {
  const hidden = !canUseAdministration();
  privilegedAdminLinks.forEach((link) => {
    link.hidden = hidden;
    link.setAttribute("aria-hidden", hidden ? "true" : "false");
    link.tabIndex = hidden ? -1 : 0;
  });
}

async function loadAdminsPage(section) {
  document.body.classList.add("admin-mode");
  try {
    const requests = [Promise.resolve({ principal: state.currentPrincipal })];
    if (section === "admins") {
      requests.push(api("/api/v1/admins"));
    } else {
      requests.push(Promise.resolve({ admins: state.admins }));
    }
    requests.push(api("/api/v1/admin-invites"));
    const [me, payload, invitesPayload] = await Promise.all(requests);
    state.currentPrincipal = me?.principal || null;
    state.admins = payload.admins || [];
    state.adminInvites = invitesPayload.invites || [];
    state.adminAccessForbidden = false;
  } catch (error) {
    if (error.status === 403) {
      state.admins = [];
      state.adminInvites = [];
      state.adminAccessForbidden = true;
    } else {
      throw error;
    }
  }
  renderAdminsPage(section);
}

function renderAdminsPage(section) {
  setAdminNavActiveSection(section);
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Администрирование</h1>
        <p class="subline">Приглашения и администраторы панели LazGate</p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
      </div>
    </section>
    ${state.adminAccessForbidden ? renderAdminsForbidden() : renderAdministrationSection(section)}
  `;
  document.querySelector("#reload").addEventListener("click", load);
  const openAdminDialogButton = document.querySelector("#open-admin-dialog");
  if (openAdminDialogButton) {
    openAdminDialogButton.addEventListener("click", openAdminDialog);
  }
  bindAdminActions();
}

function setAdminNavActiveSection(section) {
  adminNavLinks.forEach((link) => {
    link.classList.toggle("active", link.dataset.adminSection === section);
  });
}

function renderAdministrationSection(section) {
  if (section === "admins") {
    return renderAdminsTable(state.admins);
  }
  return `
    <section class="toolbar action-toolbar">
      <button id="open-admin-dialog" type="button" class="primary">Создать приглашение</button>
    </section>
    ${renderAdminInvitesTable(state.adminInvites)}
  `;
}

function renderAdminsForbiddenPage() {
  document.body.classList.add("admin-mode");
  setAdminNavActiveSection("");
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Администрирование</h1>
        <p class="subline">Приглашения и администраторы панели LazGate</p>
      </div>
    </section>
    ${renderAdminsForbidden()}
  `;
}

function renderAdminsForbidden() {
  return `
    <section class="empty">
      <strong>Недостаточно прав</strong>
      <p class="subline">Администрирование доступно только владельцам и администраторам. Пользователи с ролью просмотра не имеют доступа.</p>
    </section>
  `;
}

function renderAdminsTable(admins) {
  if (admins.length === 0) {
    return `
      <section>
        <h2>Администраторы</h2>
        <div class="empty">Администраторов пока нет.</div>
      </section>
    `;
  }
  const rows = admins.map((admin) => {
    const isLastOwner = isLastActiveOwner(admin);
    const lastOwnerHint = isLastOwner
      ? `<div class="helper admin-last-owner-hint">Последнего активного владельца нельзя понизить или отключить.</div>`
      : "";
    const passwordAction = isCurrentAdminIdentity(admin)
      ? `<button type="button" data-change-own-admin-password="${escapeAttr(admin.id)}">Сменить пароль</button>`
      : `<button type="button" data-reset-admin-password="${escapeAttr(admin.id)}">Сбросить пароль</button>`;
    return `
    <tr>
      <td data-label="Логин" class="admin-login-cell">
        <strong>${escapeHTML(admin.login || "")}</strong>
        <div class="muted mono admin-id-line">${escapeHTML(admin.id || "")}</div>
      </td>
      <td data-label="Имя">${escapeHTML(admin.display_name || "-")}</td>
      <td data-label="Роль">${renderRoleBadge(admin.role)}</td>
      <td data-label="Статус">${renderStatus(admin.status || "active")}</td>
      <td data-label="Смена пароля">${admin.must_change_password ? `<span class="status held">Требуется</span>` : `<span class="muted">Нет</span>`}</td>
      <td data-label="Последний вход">${formatAdminLastSeen(admin)}</td>
      <td data-label="Действия" class="admin-actions-cell">
        <div class="inline-actions admin-actions-list">
          <button type="button" data-edit-admin="${escapeAttr(admin.id)}">Изменить</button>
          ${passwordAction}
          <button type="button" data-revoke-admin-sessions="${escapeAttr(admin.id)}">Отозвать сессии</button>
        </div>
        ${lastOwnerHint}
      </td>
    </tr>
  `;
  }).join("");
  return `
    <section>
      <h2>Администраторы</h2>
      <div class="table-wrap">
        <table class="admin-table">
          <thead>
            <tr>
              <th>Логин</th>
              <th>Имя</th>
              <th>Роль</th>
              <th>Статус</th>
              <th>Смена пароля</th>
              <th>Последний вход</th>
              <th>Действия</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    </section>
  `;
}

function renderAdminInvitesTable(invites) {
  const filter = currentAdminInviteStatusFilter();
  const filteredInvites = invites.filter((invite) => inviteMatchesInviteFilter(invite, filter));
  const rows = filteredInvites.map((invite) => `
    <tr>
      <td data-label="Роль">${renderRoleBadge(invite.role)}</td>
      <td data-label="Статус">${renderInviteStatus(invite)}</td>
      <td data-label="Истекает">${formatDateTime(invite.expires_at)}</td>
      <td data-label="Кто создал">${escapeHTML(invite.invited_by_login_snapshot || "-")}</td>
      <td data-label="Кто использовал">${renderInviteUsedBy(invite)}</td>
      <td data-label="Заметка">${escapeHTML(invite.note || "-")}</td>
      <td data-label="Действия">
        ${invite.status === "active" && !isInviteExpired(invite) ? `<button type="button" data-revoke-admin-invite="${escapeAttr(invite.id)}">Отозвать</button>` : `<span class="muted">—</span>`}
      </td>
    </tr>
  `).join("");
  const table = filteredInvites.length === 0
    ? `<section class="empty">Приглашений с выбранным статусом нет.</section>`
    : `
      <div class="table-wrap">
        <table class="admin-table">
          <thead><tr><th>Роль</th><th>Статус</th><th>Истекает</th><th>Кто создал</th><th>Кто использовал</th><th>Заметка</th><th>Действия</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    `;
  return `
    <section>
      <div class="toolbar invite-filter-toolbar">
        <h2>Приглашения</h2>
        <div class="invite-filter-tabs" role="group" aria-label="Фильтр приглашений по статусу">
          ${ADMIN_INVITE_FILTERS.map((item) => `<button type="button" class="${item.value === filter ? "active" : ""}" data-invite-filter="${escapeAttr(item.value)}">${escapeHTML(item.label)}</button>`).join("")}
        </div>
      </div>
      ${table}
    </section>
  `;
}

function inviteMatchesInviteFilter(invite, filter) {
  if (filter === "all") {
    return true;
  }
  if (filter === "expired") {
    return isInviteExpired(invite);
  }
  if (filter === "active") {
    return invite.status === "active" && !isInviteExpired(invite);
  }
  return invite.status === filter;
}

function currentAdminInviteStatusFilter() {
  const queryFilter = new URLSearchParams(location.search).get("status") || "";
  return isAdminInviteStatusFilter(queryFilter) ? queryFilter : DEFAULT_ADMIN_INVITE_STATUS_FILTER;
}

function setAdminInviteStatusFilter(filter) {
  const nextFilter = isAdminInviteStatusFilter(filter) ? filter : DEFAULT_ADMIN_INVITE_STATUS_FILTER;
  const url = new URL(location.href);
  url.searchParams.set("status", nextFilter);
  history.pushState(null, "", `${url.pathname}${url.search}${url.hash}`);
}

function isAdminInviteStatusFilter(filter) {
  return ADMIN_INVITE_FILTERS.some((item) => item.value === filter);
}

function isInviteExpired(invite) {
  return invite.status === "active" && Date.parse(invite.expires_at || "") <= Date.now();
}

function renderInviteStatus(invite) {
  if (isInviteExpired(invite)) {
    return renderStatus("expired");
  }
  return renderStatus(invite.status || "active");
}

function renderInviteUsedBy(invite) {
  if (invite.status !== "used") {
    return `<span class="muted">—</span>`;
  }
  const login = invite.used_by_login || invite.used_by_identity_id || "-";
  const displayName = invite.used_by_display_name || "";
  return `
    <strong>${escapeHTML(login)}</strong>
    ${displayName ? `<div class="muted">${escapeHTML(displayName)}</div>` : ""}
  `;
}

function renderAdminDialogBody() {
  return `
    <form id="admin-form" method="dialog" class="laz-modal-fields">
      <label>
        Роль
        <select name="role" required>${adminRoleOptions("admin")}</select>
      </label>
      <label>
        TTL
        <select name="ttl" required>
          <option value="24h">24 часа</option>
          <option value="72h">3 дня</option>
          <option value="168h">7 дней</option>
        </select>
      </label>
      <label>
        Заметка
        <input name="note" autocomplete="off">
      </label>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="submit" class="primary">Создать приглашение</button>
      </div>
    </form>
  `;
}

function openAdminDialog() {
  const dialog = window.lazUI.openModal({
    id: "admin-dialog",
    title: "Создать приглашение",
    body: renderAdminDialogBody(),
  });
  const form = dialog.querySelector("#admin-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    try {
      const result = await api("/api/v1/admin-invites", {
        method: "POST",
        body: {
          role: String(data.get("role") || "admin"),
          ttl: String(data.get("ttl") || "24h"),
          note: String(data.get("note") || "").trim(),
        },
      });
      form.reset();
      dialog.close();
      showInviteCreatedDialog(result);
      await loadAdminsPage("invites");
    } catch (error) {
      showToast(error.message || "Не удалось создать приглашение");
    }
  });
}

function showInviteCreatedDialog(result) {
  window.lazUI.openModal({
    id: "invite-result-dialog",
    title: "Приглашение создано",
    body: `
      <div class="laz-modal-fields">
        <label>URL<input readonly value="${escapeAttr(result.invite_url || "")}"></label>
        <button type="button" data-copy-value="${escapeAttr(result.invite_url || "")}">Скопировать URL</button>
        <label>Код<input readonly value="${escapeAttr(result.invite_code || "")}"></label>
        <button type="button" data-copy-value="${escapeAttr(result.invite_code || "")}">Скопировать код</button>
        <p class="helper">Код показывается только один раз.</p>
        <div class="laz-modal-actions"><button type="button" data-close-dialog>Закрыть</button></div>
      </div>
    `,
  });
  document.querySelectorAll("[data-copy-value]").forEach((button) => {
    button.addEventListener("click", () => copyText(button.dataset.copyValue));
  });
}

function bindAdminActions() {
  app.querySelectorAll("[data-edit-admin]").forEach((button) => {
    button.addEventListener("click", () => openEditAdminDialog(button.dataset.editAdmin));
  });
  app.querySelectorAll("[data-reset-admin-password]").forEach((button) => {
    button.addEventListener("click", () => openResetAdminPasswordDialog(button.dataset.resetAdminPassword));
  });
  app.querySelectorAll("[data-change-own-admin-password]").forEach((button) => {
    button.addEventListener("click", () => openChangeOwnAdminPasswordDialog(button.dataset.changeOwnAdminPassword));
  });

  app.querySelectorAll("[data-invite-filter]").forEach((button) => {
    button.addEventListener("click", () => {
      setAdminInviteStatusFilter(button.dataset.inviteFilter || DEFAULT_ADMIN_INVITE_STATUS_FILTER);
      renderAdminsPage("invites");
    });
  });

  app.querySelectorAll("[data-revoke-admin-invite]").forEach((button) => {
    button.addEventListener("click", async () => {
      await api(`/api/v1/admin-invites/${encodeURIComponent(button.dataset.revokeAdminInvite)}/revoke`, { method: "POST" });
      showToast("Приглашение отозвано");
      await loadAdminsPage("invites");
    });
  });

  app.querySelectorAll("[data-revoke-admin-sessions]").forEach((button) => {
    button.addEventListener("click", () => {
      const admin = findAdmin(button.dataset.revokeAdminSessions);
      openConfirmDialog({
        title: "Отозвать сессии?",
        message: `Все активные сессии администратора ${admin?.login || ""} будут завершены.`,
        confirmLabel: "Отозвать сессии",
        onConfirm: async () => {
          await revokeAdminSessions(button.dataset.revokeAdminSessions);
        },
      });
    });
  });
}

function openEditAdminDialog(adminID, values = null) {
  const admin = findAdmin(adminID);
  if (!admin) {
    showToast("Администратор не найден");
    return;
  }
  const isLastOwner = isLastActiveOwner(admin);
  const initial = {
    display_name: values?.display_name ?? admin.display_name ?? "",
    role: values?.role ?? admin.role ?? "admin",
    status: values?.status ?? admin.status ?? "active",
  };
  const lastOwnerHint = isLastOwner
    ? `<section class="notice warn">Это последний активный владелец. Понижение роли и отключение недоступны в интерфейсе; сервер также отклонит такую операцию.</section>`
    : "";
  const dialog = window.lazUI.openModal({
    id: "edit-admin-dialog",
    title: `Изменить ${admin.login || "администратора"}`,
    body: `
      <form id="edit-admin-form" method="dialog" class="laz-modal-fields">
        ${lastOwnerHint}
        <label>
          Отображаемое имя
          <input name="display_name" autocomplete="off" value="${escapeAttr(initial.display_name)}">
        </label>
        <div class="grid-two">
          <label>
            Роль
            <select name="role" required>${adminRoleOptions(initial.role, isLastOwner)}</select>
          </label>
          <label>
            Статус
            <select name="status" required>${adminStatusOptions(initial.status, isLastOwner)}</select>
          </label>
        </div>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="primary">Сохранить</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#edit-admin-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const body = {
      display_name: String(data.get("display_name") || "").trim(),
      role: String(data.get("role") || "admin"),
      status: String(data.get("status") || "active"),
    };
    const changedRole = body.role !== (admin.role || "admin");
    const changedStatus = body.status !== (admin.status || "active");
    const isDangerousRoleChange = (admin.role || "") === "owner" && body.role !== "owner";
    const isDangerousStatusChange = (admin.status || "active") === "active" && body.status !== "active";
    const isDangerousChange = isDangerousRoleChange || isDangerousStatusChange;
    const saveAdmin = async () => {
      try {
        await updateAdmin(admin.id, body);
        showToast("Администратор обновлен");
        await loadAdminsPage("admins");
      } catch (error) {
        showToast(error.message || "Не удалось обновить администратора");
      }
    };
    if (!isDangerousChange || (!changedRole && !changedStatus)) {
      dialog.close();
      await saveAdmin();
      return;
    }
    dialog.close();
    openConfirmDialog({
      title: "Подтвердить изменение доступа?",
      message: adminAccessChangeMessage(admin, body),
      confirmLabel: "Сохранить изменение",
      onConfirm: saveAdmin,
      onCancel: () => openEditAdminDialog(admin.id, body),
    });
  });
}

function openChangeOwnAdminPasswordDialog(adminID) {
  const admin = findAdmin(adminID);
  if (!admin || !isCurrentAdminIdentity(admin)) {
    showToast("Администратор не найден");
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "change-own-admin-password-dialog",
    title: "Сменить пароль",
    body: `
      <form id="change-own-admin-password-form" method="dialog" class="laz-modal-fields">
        <label>
          Текущий пароль
          <input name="current_password" type="password" required autocomplete="current-password">
        </label>
        <label>
          Новый пароль
          <input name="new_password" type="password" required autocomplete="new-password" minlength="6" pattern="(?=.*[A-Za-z])(?=.*\\d)(?=.*[^A-Za-z0-9]).{6,}" title="Минимум 6 символов: буква, цифра и символ">
        </label>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="primary">Сменить пароль</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#change-own-admin-password-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    try {
      await api(`/api/v1/admins/${encodeURIComponent(admin.id)}/reset-password`, {
        method: "POST",
        body: {
          current_password: String(data.get("current_password") || ""),
          new_password: String(data.get("new_password") || ""),
        },
      });
      form.reset();
      dialog.close();
      showToast("Пароль изменен");
      await loadAdminsPage("admins");
    } catch (error) {
      showToast(error.message || "Не удалось изменить пароль");
    }
  });
}

function openResetAdminPasswordDialog(adminID) {
  const admin = findAdmin(adminID);
  if (!admin) {
    showToast("Администратор не найден");
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "reset-admin-password-dialog",
    title: `Сбросить пароль ${admin.login || ""}`,
    body: `
      <form id="reset-admin-password-form" method="dialog" class="laz-modal-fields">
        <section class="notice warn">После сброса администратор должен будет сменить пароль при входе, а его сессии будут отозваны.</section>
        <label>
          Новый временный пароль
          <input name="password" type="password" required autocomplete="new-password" minlength="6" pattern="(?=.*[A-Za-z])(?=.*\\d)(?=.*[^A-Za-z0-9]).{6,}" title="Минимум 6 символов: буква, цифра и символ">
        </label>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="danger">Сбросить пароль</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#reset-admin-password-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const password = String(data.get("password") || "");
    dialog.close();
    openConfirmDialog({
      title: "Сбросить пароль?",
      message: `Администратор ${admin.login || ""} должен будет сменить пароль при следующем входе. Все его сессии будут отозваны.`,
      confirmLabel: "Сбросить пароль",
      onConfirm: async () => {
        try {
          await api(`/api/v1/admins/${encodeURIComponent(admin.id)}/reset-password`, {
            method: "POST",
            body: { password },
          });
          showToast("Пароль сброшен, сессии отозваны");
          await loadAdminsPage("admins");
        } catch (error) {
          showToast(error.message || "Не удалось сбросить пароль");
        }
      },
      onCancel: () => openResetAdminPasswordDialog(admin.id),
    });
  });
}


async function updateAdmin(adminID, body) {
  await api(`/api/v1/admins/${encodeURIComponent(adminID)}`, {
    method: "PATCH",
    body,
  });
}

async function revokeAdminSessions(adminID) {
  await api(`/api/v1/admins/${encodeURIComponent(adminID)}/revoke-sessions`, { method: "POST" });
  showToast("Сессии администратора отозваны");
  await loadAdminsPage("admins");
}

function findAdmin(adminID) {
  return state.admins.find((admin) => admin.id === adminID) || null;
}

function isCurrentAdminIdentity(admin) {
  const principal = state.currentPrincipal || {};
  return Boolean(admin && ((principal.identity_id && admin.id === principal.identity_id) || admin.login === principal.name));
}

function isLastActiveOwner(admin) {
  if (!admin || admin.role !== "owner" || (admin.status || "active") !== "active") {
    return false;
  }
  return state.admins.filter((item) => item.role === "owner" && (item.status || "active") === "active").length === 1;
}

function formatAdminLastSeen(admin) {
  if (!admin) {
    return `<span class="muted">—</span>`;
  }
  const value = admin.last_seen_at || admin.last_seen || admin.last_login_at;
  const formatted = admin.last_seen_ms ? relativeTime(admin.last_seen_ms) : formatDateTime(value);
  const onlineText = admin.online ? ` <span class="muted">online</span>` : "";
  return `${formatted}${onlineText}`;
}

function adminAccessChangeMessage(admin, body) {
  const changes = [];
  if ((admin.role || "") === "owner" && body.role !== "owner") {
    changes.push(`роль изменится на «${adminRoleLabel(body.role)}»`);
  }
  if ((admin.status || "active") === "active" && body.status !== "active") {
    changes.push(`статус изменится на «${statusLabel(body.status)}»`);
  }
  const suffix = isLastActiveOwner(admin)
    ? " Это выглядит как последний активный владелец; сервер может отклонить операцию."
    : "";
  return `Для администратора ${admin.login || ""} ${changes.join(", ")}. Продолжить?${suffix}`;
}

function adminRoleOptions(selected, protectOwner = false) {
  return ["owner", "admin", "viewer"].map((role) => {
    const disabled = protectOwner && role !== "owner" ? "disabled" : "";
    return `<option value="${escapeAttr(role)}" ${role === selected ? "selected" : ""} ${disabled}>${escapeHTML(adminRoleLabel(role))}</option>`;
  }).join("");
}

function adminStatusOptions(selected, protectOwner = false) {
  return ["active", "deleted"].map((status) => {
    const disabled = protectOwner && status !== "active" ? "disabled" : "";
    return `<option value="${escapeAttr(status)}" ${status === selected ? "selected" : ""} ${disabled}>${statusLabel(status)}</option>`;
  }).join("");
}

function renderRoleBadge(role) {
  const value = role || "viewer";
  const cls = value === "owner" ? "owner" : value === "admin" ? "active" : "held";
  return `<span class="status ${cls}">${escapeHTML(adminRoleLabel(value))}</span>`;
}

function adminRoleLabel(role) {
  switch (role) {
    case "owner":
      return "Владелец";
    case "admin":
      return "Админ";
    case "viewer":
      return "Просмотр";
    default:
      return role || "-";
  }
}

function startEvents() {
  stopEvents();
  if (!state.csrfToken || document.visibilityState === "hidden") {
    return;
  }
  const source = new EventSource(adminAPIPath("/api/v1/events"));
  state.eventSource = source;
  source.onmessage = (event) => handleServerEvent(event);
  [
    "hysteria.install.scheduled",
    "awg.install.scheduled",
    "awg.install.applied",
    "awg.install.failed",
    "awg.install.done",
    "hysteria.install.step.applied",
    "hysteria.install.step.failed",
    "hysteria.install.done",
    "hysteria.uninstall.scheduled",
    "hysteria.uninstall.done",
    "hysteria.uninstall.failed",
    "hysteria.detach.scheduled",
    "hysteria.detach.done",
    "hysteria.detach.failed",
    "connection.applied",
    "node.created",
  ].forEach((type) => {
    source.addEventListener(type, handleServerEvent);
  });
  source.onerror = () => {
    // EventSource reconnects automatically; keep the UI quiet unless a concrete event arrives.
  };
}

function stopEvents() {
  if (state.eventSource) {
    state.eventSource.close();
    state.eventSource = null;
  }
}

function handleVisibilityChange() {
  if (document.visibilityState === "hidden") {
    stopRefreshTimer();
    stopEvents();
    return;
  }
  handleVisiblePage();
}

async function handleVisiblePage() {
  if (!state.csrfToken || document.visibilityState === "hidden") {
    return;
  }
  await refreshAdminAuthIfDue();
  scheduleAdminRefresh();
  startEvents();
}

function applyAuthTimings(payload) {
  if (payload?.csrf_token) {
    state.csrfToken = payload.csrf_token;
    localStorage.setItem(csrfKey, state.csrfToken);
  }
  if (payload?.refresh_after) {
    state.refreshAfterMS = Date.parse(payload.refresh_after);
    localStorage.setItem(refreshAfterKey, payload.refresh_after);
  }
  scheduleAdminRefresh();
}

function clearAdminAuthStorage() {
  localStorage.removeItem(csrfKey);
  localStorage.removeItem(refreshAfterKey);
}

function expireAdminSession() {
  clearAdminAuthStorage();
  state.csrfToken = "";
  state.refreshAfterMS = 0;
  stopRefreshTimer();
  stopEvents();
  window.lazUI.closeModal?.();
  location.replace(`${state.webPrefix}/login`);
}

function scheduleAdminRefresh() {
  stopRefreshTimer();
  if (!state.csrfToken || !state.refreshAfterMS || document.visibilityState === "hidden") {
    return;
  }
  const delay = Math.max(0, state.refreshAfterMS - Date.now());
  state.refreshTimer = window.setTimeout(() => {
    state.refreshTimer = 0;
    refreshAdminAuth();
  }, delay);
}

function stopRefreshTimer() {
  if (state.refreshTimer) {
    window.clearTimeout(state.refreshTimer);
    state.refreshTimer = 0;
  }
}

async function refreshAdminAuthIfDue() {
  if (!state.refreshAfterMS || Date.now() < state.refreshAfterMS) {
    return;
  }
  await refreshAdminAuth();
}

async function refreshAdminAuth() {
  try {
    const payload = await api("/api/v1/auth/refresh", { method: "POST" });
    applyAuthTimings(payload);
  } catch (error) {
    if (error?.status === 401) {
      expireAdminSession();
      return;
    }
    scheduleAdminRefresh();
  }
}

function handleServerEvent(event) {
  let payload = {};
  try {
    payload = JSON.parse(event.data || "{}");
  } catch (_) {
    return;
  }
  if (payload.message) {
    recordDisplayedEvent(payload);
    showToast(payload.message);
  }
  const detail = payload.payload || {};
  if (detail.operation_id) state.installProgress?.event(payload);
  scheduleReloadForEvent(payload);
}

function recordDisplayedEvent(payload) {
  state.eventHistory.push({
    time: new Date().toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
    type: String(payload.type || ""),
    message: String(payload.message || ""),
  });
  if (state.eventHistory.length > MAX_EVENT_HISTORY) {
    state.eventHistory.splice(0, state.eventHistory.length - MAX_EVENT_HISTORY);
  }
  renderEventHistoryLink();
}

function renderEventHistoryLink() {
  if (!eventHistoryLink || !eventHistoryCount) {
    return;
  }
  eventHistoryCount.textContent = String(state.eventHistory.length);
  eventHistoryLink.disabled = state.eventHistory.length === 0;
}

function openEventHistory() {
  const items = state.eventHistory.slice().reverse();
  const body = items.length
    ? `<ol class="event-history-list">${items.map(renderEventHistoryItem).join("")}</ol>`
    : `<p class="subline">За время открытия страницы события не отображались.</p>`;
  window.lazUI.openModal({
    id: "event-history-dialog",
    title: "Отображенные события",
    body,
    wide: true,
  });
}

function renderEventHistoryItem(item) {
  const type = item.type ? `<span class="event-history-type">${escapeHTML(item.type)}</span>` : "";
  return `
    <li class="event-history-item">
      <time>${escapeHTML(item.time)}</time>
      <span>${escapeHTML(item.message)}</span>
      ${type}
    </li>
  `;
}

function scheduleReloadForEvent(event) {
  if (!["node", "account", "connection", "hysteria_install", "awg_install"].includes(event.entity_type || "")) {
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
        <p class="subline">Сводка за выбранный период</p>
      </div>
      <div class="toolbar nav-toolbar">
        <select id="dashboard-range" class="range-select" aria-label="Период сводки">
          <option value="24h"${state.dashboardRange === "24h" ? " selected" : ""}>Последние 24 часа</option>
          <option value="7d"${state.dashboardRange === "7d" ? " selected" : ""}>Последние 7 дней</option>
          <option value="30d"${state.dashboardRange === "30d" ? " selected" : ""}>Последние 30 дней</option>
        </select>
        <button id="reload" type="button">Обновить</button>
      </div>
    </section>
    <section class="summary-grid">
      ${serverHealthCard(summary)}
      ${summaryCard("Пользователи онлайн", String(summary.online_users || 0))}
      ${summaryCard("Всего трафика", formatBytes(summary.total_traffic_bytes || 0))}
      ${availabilityCard(summary)}
      ${summary.offline_duration_ms ? summaryCard("Время офлайн", formatDuration(summary.offline_duration_ms)) : ""}
    </section>
    <section class="dashboard-card">
      <div class="card-head">
        <h2>Динамика трафика</h2>
        <span class="muted">${escapeHTML(({ hour: "По часам", day: "По дням", week: "По неделям" })[(data.range || {}).bucket] || "Автоматически")}</span>
      </div>
      <div class="traffic-chart" id="dashboard-traffic-chart"></div>
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2 id="dashboard-servers" tabindex="-1">Обзор серверов</h2></div>
      ${renderDashboardNodes([...(data.nodes || [])].sort((a, b) => Number(a.status === "online") - Number(b.status === "online")))}
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2>Активные подключения</h2></div>
      ${renderDashboardOnlineUsers(data.online_users || [])}
    </section>
    <section class="ranking-grid">
      ${renderRankingCard("Top пользователей по трафику", data.top_users_by_traffic || [], "display_name", "traffic_bytes")}
      ${renderRankingCard("Трафик по серверам", data.traffic_by_node || [], "name", "traffic_bytes")}
    </section>
    <section class="dashboard-card">
      <div class="card-head"><h2>Downtime</h2></div>
      ${renderDowntime(data.downtime || [])}
    </section>
  `;
  const chart = app.querySelector("#dashboard-traffic-chart");
  const drawChart = (width) => {
    if (chart.isConnected && width > 0) {
      chart.innerHTML = renderTrafficChart(data.traffic_over_time || [], width);
    }
  };
  drawChart(chart.getBoundingClientRect().width);
  dashboardChartObserver?.disconnect();
  dashboardChartObserver = new ResizeObserver(([entry]) => drawChart(entry.contentRect.width));
  dashboardChartObserver.observe(chart);
  app.querySelectorAll('[href="#dashboard-servers"]').forEach((link) => {
    link.addEventListener("click", (event) => {
      event.preventDefault();
      const heading = app.querySelector("#dashboard-servers");
      heading.focus({ preventScroll: true });
      heading.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  });
  document.querySelector("#reload").addEventListener("click", load);
  document.querySelector("#dashboard-range").addEventListener("change", async (event) => {
    state.dashboardRange = event.target.value;
    await load();
  });
}

function serverHealthCard(summary) {
  const total = summary.total_nodes || 0;
  const online = summary.online_nodes || 0;
  const offline = Math.max(0, total - online);
  if (!total) return summaryCard("Серверы онлайн", "—", { detail: "Серверы не добавлены" });
  const detail = !offline ? "Все серверы доступны" : !online ? "Все серверы недоступны" : `Недоступно серверов: ${offline}`;
  return summaryCard("Серверы онлайн", `${online} / ${total}`, {
    tone: !offline ? "good" : !online ? "danger" : "warning",
    detail, link: offline > 0,
  });
}

function availabilityCard(summary) {
  if (!summary.total_nodes || !Number.isFinite(summary.availability_percent)) {
    return summaryCard("Доступность", "—", { detail: "Нет данных за выбранный период" });
  }
  const value = summary.availability_percent;
  const tone = value >= 100 ? "good" : value >= 99 ? "warning" : "danger";
  const detail = value >= 100 ? "Без простоев за выбранный период" : "Были простои за выбранный период";
  const formatted = formatPercent(value);
  return summaryCard("Доступность", `${value < 100 && Number(formatted) >= 100 ? "< 100" : formatted}%`, { tone, detail, link: value < 100 });
}

function summaryCard(label, value, { tone = "", detail = "", link = false } = {}) {
  const tag = link ? "a" : "article";
  return `
    <${tag} class="summary-card${tone ? ` summary-${tone}` : ""}"${link ? ' href="#dashboard-servers"' : ""}>
      <span>${escapeHTML(label)}</span>
      <strong>${escapeHTML(value)}</strong>
      ${detail ? `<small class="summary-detail">${tone === "warning" || tone === "danger" ? '<b class="summary-alert" aria-hidden="true">!</b>' : ""}${escapeHTML(detail)}</small>` : ""}
    </${tag}>
  `;
}

function renderTrafficChart(items, width) {
  if (items.length === 0 || items.every((item) => !item.total_bytes)) {
    return `<section class="empty compact">Нет трафика за выбранный период.</section>`;
  }
  const height = 220;
  const pad = 24;
  const maxValue = Math.max(...items.map((item) => item.total_bytes || 0), 1);
  const points = items.map((item, index) => {
    const x = pad + ((index + 0.5) / items.length) * (width - pad * 2);
    const y = height - pad - ((item.total_bytes || 0) / maxValue) * (height - pad * 2);
    return [x, y];
  });
  const line = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `${pad},${height - pad} ${line} ${width - pad},${height - pad}`;
  const step = (width - pad * 2) / items.length;
  const labelStride = Math.max(1, Math.ceil(85 / step));
  const ticks = Array.from({ length: items.length + 1 }, (_, index) => {
    const x = pad + index * step;
    return `<line class="chart-grid" x1="${x}" x2="${x}" y1="${pad}" y2="${height - pad + 5}"></line>`;
  }).join("");
  const labels = items.map((item, index) => {
    if (index % labelStride !== 0 || (index > 0 && index * step + 80 > width - pad * 2)) return "";
    return `<text x="${pad + index * step}" y="${height - 5}" text-anchor="start">${escapeHTML(item.bucket_label || "")}</text>`;
  }).join("");
  return `
      <svg viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" role="img" aria-label="Динамика трафика">
        <polyline class="chart-grid" points="${pad},${pad} ${pad},${height - pad} ${width - pad},${height - pad}"></polyline>
        <polygon class="chart-area" points="${area}"></polygon>
        ${ticks}
        <polyline class="chart-line" points="${line}"></polyline>
        ${labels}
        <text x="${width - pad}" y="${pad - 8}" text-anchor="end">${escapeHTML(formatBytes(maxValue))}</text>
      </svg>
  `;
}

function renderDashboardNodes(nodes) {
  if (nodes.length === 0) {
    return `<section class="empty compact">Серверы пока не подключены.</section>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Сервер</th><th>Статус</th><th>Сервис</th><th>Онлайн</th><th>Трафик</th><th>Доступность</th><th>Офлайн</th><th>Последний heartbeat</th></tr></thead>
        <tbody>
          ${nodes.map((node) => `
            <tr>
              <td><a class="row-link" href="${state.webPrefix}/nodes/${encodeURIComponent(node.node_id)}" data-link>${escapeHTML(node.name || node.node_id)}</a></td>
              <td>${renderNodeStatus(node.status)}</td>
              <td>${escapeHTML(node.service_status || "unknown")}</td>
              <td>${escapeHTML(String(node.online_users || 0))} польз. · ${escapeHTML(String(node.online_connections || 0))} подкл.</td>
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
    return `<section class="empty compact">Сейчас нет активных подключений.</section>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Пользователь</th><th>Серверы</th><th>Подключения</th><th>Трафик</th><th>Последняя активность</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td>${escapeHTML(item.display_name || item.connection_id)}<div class="muted mono">${escapeHTML((item.native_auth_ids || []).join(", "))}</div></td>
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
              <span>${escapeHTML(item[labelField] || item.connection_id || item.node_id || "")}</span>
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
    return `<section class="empty compact">Все серверы были онлайн за выбранный период.</section>`;
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
      `}
    </section>
    ${renderAccountsTable(state.accounts)}
    <section class="nodes-footer">
      <strong>Серверы</strong>
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

function renderNodesPage() {
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Серверы</h1>
        <p class="subline">${state.nodes.length} ${plural(state.nodes.length, "сервер", "сервера", "серверов")}</p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
      </div>
    </section>
    <section class="toolbar action-toolbar">
      <button id="open-hysteria-node-dialog" type="button" class="primary">Установить Hysteria2</button>
      <button id="open-awg-node-dialog" type="button">Установить AmneziaWG</button>
      <button id="attach-hysteria-node-dialog" type="button">Подключить Hysteria2</button>
      <button id="open-amnezia-node-dialog" type="button">Добавить Amnezia API</button>
    </section>
    ${renderNodesTable(state.nodes)}
  `;
  document.querySelector("#reload").addEventListener("click", load);
  document.querySelector("#open-hysteria-node-dialog").addEventListener("click", () => openHysteriaNodeDialog("install"));
  document.querySelector("#open-awg-node-dialog").addEventListener("click", openAWGNodeDialog);
  document.querySelector("#attach-hysteria-node-dialog").addEventListener("click", () => openHysteriaNodeDialog("attach"));
  document.querySelector("#open-amnezia-node-dialog").addEventListener("click", openAmneziaNodeDialog);
}

function renderAmneziaNodeDialogBody() {
  return `
    <form id="amnezia-node-form" method="dialog" class="laz-modal-fields">
      <section class="notice">
        Добавьте существующий Amnezia API endpoint. LazGate будет использовать Base URL и API key для управления AmneziaWG подключениями.
      </section>
      <label>
        Имя сервера
        <input name="name" required autocomplete="off">
      </label>
      <label>
        Base URL
        <input name="base_url" type="url" required autocomplete="off" placeholder="http://127.0.0.1:4001">
      </label>
      <label>
        API key
        <input name="api_key" type="password" required autocomplete="new-password">
      </label>
      <label>
        Регион
        <input name="region" autocomplete="off" placeholder="FI-Helsinki">
      </label>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="submit" class="primary">Добавить Amnezia API</button>
      </div>
    </form>
  `;
}

function openAmneziaNodeDialog() {
  const dialog = window.lazUI.openModal({
    id: "amnezia-node-dialog",
    title: "Добавить Amnezia API",
    body: renderAmneziaNodeDialogBody(),
  });
  const form = dialog.querySelector("#amnezia-node-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const name = String(data.get("name") || "").trim();
    const baseURL = String(data.get("base_url") || "").trim();
    const apiKey = String(data.get("api_key") || "");
    const region = String(data.get("region") || "").trim();
    try {
      await api("/api/v1/nodes", {
        method: "POST",
        body: {
          name,
          type: AMNEZIA_NODE_TYPE,
          amnezia_api_base_url: baseURL,
          amnezia_api_key: apiKey,
          region,
        },
      });
      dialog.close();
      showToast("Сервер Amnezia добавлен");
      await load();
    } catch (error) {
      showToast(error.message || "Не удалось добавить Amnezia API");
    }
  });
}

function renderNodesTable(nodes) {
  if (nodes.length === 0) {
    return `<section class="empty">Серверов пока нет.</section>`;
  }
  const rows = nodes.map((node) => `
    <tr>
      <td data-label="Имя">
        <a class="row-link" href="${state.webPrefix}/nodes/${encodeURIComponent(node.id)}" data-link>${escapeHTML(node.name || node.id)}</a>
      </td>
      <td data-label="Тип/протокол">
        ${escapeHTML(nodeTypeLabel(node.type))}
        <div class="muted">${escapeHTML(protocolForNode(node) || "-")}</div>
      </td>
      <td data-label="Статус">${renderStatus(node.status || "active")}</td>
      <td data-label="Регион">${escapeHTML(node.region || "-")}</td>
      <td data-label="Instance">${escapeHTML(node.instance?.host || "-")}</td>
      <td data-label="Agent">${escapeHTML(node.agent_status || "-")}</td>
      <td data-label="Создан">${formatDateTime(node.created_at)}</td>
    </tr>
  `).join("");
  return `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Имя</th>
            <th>Тип/протокол</th>
            <th>Статус</th>
            <th>Регион</th>
            <th>Instance</th>
            <th>Agent</th>
            <th>Создан</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function isAccountsPage() {
  return location.pathname === `${state.webPrefix}/accounts` || location.pathname === `${state.webPrefix}/accounts/`;
}

function isNodesPage() {
  return location.pathname === `${state.webPrefix}/nodes` || location.pathname === `${state.webPrefix}/nodes/`;
}

function renderServerIdentityFields() {
  return `<div class="grid-two">
    <label>Имя сервера *<input name="node_name" required autocomplete="off"></label>
    <label>Регион<select name="region_choice"><option value="auto">Автоматически</option></select><input name="region" type="hidden" value=""></label>
  </div>`;
}

function renderServerSSHFields(mode = "install") {
  return `
    <fieldset class="node-install-settings">
      <legend>SSH-доступ к серверу</legend>
      <div class="grid-two">
        <label>IP *<input name="ssh_host" required autocomplete="off" spellcheck="false"></label>
        <label>Порт *<input name="ssh_port" type="number" min="1" max="65535" value="22" required></label>
      </div>
      <div class="grid-two">
        <label>Пользователь *<input name="bootstrap_user" value="root" required autocomplete="off"></label>
        <label>Пароль *<input name="bootstrap_password" type="password" required autocomplete="new-password"></label>
      </div>
      <div class="notice warn">Поля «Пользователь» и «Пароль» нужны только для начала ${mode === "attach" ? "подключения" : "установки"}. Их значения не сохраняются.</div>
    </fieldset>
  `;
}

function renderAWGNodeDialogBody() {
  return `<form id="awg-node-form" method="dialog" class="laz-modal-fields">
      ${renderServerIdentityFields()}
      ${renderServerSSHFields()}
      <fieldset class="node-install-settings">
        <legend>Настройки AmneziaWG</legend>
        ${window.LazAmneziaSettings.renderFields(window.LazAmneziaSettings.defaults())}
        <div class="notice">AmneziaWG 3.1: Ubuntu 24.04 или 26.04, amd64 (x86-64), ядро 6.7 или новее. Интерфейс awg0. Подсеть не должна пересекаться с существующими сетями.</div>
        <div class="notice">Разрешите входящий UDP-трафик на выбранный порт в firewall провайдера. Доступность VPN с вашего устройства проверяется после установки.</div>
        <label class="checkbox-line"><input name="allow_package_upgrade" type="checkbox" required>Разрешить обновление пакетов ОС и перезагрузки сервера *</label>
      </fieldset>
      <div id="awg-install-progress" role="status" aria-live="polite"></div>
      <div class="laz-modal-actions"><button type="button" data-close-dialog>Отмена</button><button type="submit" class="primary">Установить</button></div>
    </form>`;
}

function openAWGNodeDialog() {
  const dialog = window.lazUI.openModal({
    id: "awg-node-dialog",
    title: "Установить AmneziaWG",
    body: renderAWGNodeDialogBody(),
    wide:true,className:"awg-settings-dialog",
  });
  const form = dialog.querySelector("form");
  const serverFields = window.lazServerFields.bind(form);
  form.noValidate=true;
  window.LazAmneziaSettings.bind(dialog,form,window.LazAmneziaSettings.defaults(),
    (body,signal)=>api("/api/v1/nodes/amneziawg-signatures",{method:"POST",body,signal}),{install:true});
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const submit = form.querySelector('[type="submit"]');
    if (submit.disabled) return;
    submit.disabled = true;
    if (!window.LazAmneziaSettings.validate(form) || !await serverFields.prepare()) {
      submit.disabled = false;
      return;
    }
    const data = new FormData(form);
    const body={settings:window.LazAmneziaSettings.read(form),allow_package_upgrade:Boolean(data.get("allow_package_upgrade")),ssh_port:Number(data.get("ssh_port"))};
    for(const key of ["node_name","region","ssh_host","bootstrap_user","bootstrap_password"])body[key]=String(data.get(key)||"");
    const target = form.querySelector("#awg-install-progress");
    const names = [
      ["prepare", "Подготовка сервера"],
      ["launch", "Запуск установщика"],
      ["poll", "Обновление системы и установка AmneziaWG"],
      ["handoff", "Настройка AmneziaWG"],
      ["register", "Регистрация сервера"],
      ["agent", "Установка LazGate Agent"],
      ["verify", "Проверка сервисов"],
      ["wait_agent", "Ожидание LazGate Agent"],
      ["done", "Готово"],
    ];
    const progress = new window.LazInstallProgress(target, names.map(([type, name]) => ({
      type: "awg.install." + type, name,
    })), state.webPrefix);
    state.installProgress = progress;
    try {
      const result = await api("/api/v1/nodes/install-amneziawg", { method: "POST", body });
      progress.accept(result);
      form.elements.bootstrap_password.value = "";
      form.elements.header_protection_key.value="";
      form.querySelectorAll("input, select, textarea, [data-awg-settings] button").forEach((input) => { input.disabled = true; });
      submit.hidden = true;
      form.querySelector("[data-close-dialog]").textContent = "Закрыть";
    } catch (error) {
      progress.fail(error.message || String(error));
      submit.disabled = false;
    }
  });
}

function renderHysteriaNodeDialogBody(mode = "install") {
  const isAttach = mode === "attach";
  return `
    <form id="hysteria-node-form" method="dialog" class="laz-modal-fields">
      ${renderServerIdentityFields()}
      ${renderServerSSHFields(mode)}
      ${isAttach ? `
        <section class="notice warn">
          Текущий /etc/hysteria/config.yaml будет сохранён рядом как lazgate-backup, после чего Hysteria2 будет переведена на авторизацию через LazGate и LazGate Agent.
        </section>
      ` : ""}
      <fieldset class="node-install-settings">
        <legend>Настройки Hysteria2</legend>
        <div class="public-domain-field">
          <label for="hysteria-public-domain">Публичный домен *</label>
          <label class="checkbox-line"><input name="domain_auto" type="checkbox" checked>Автоматически</label>
          <input id="hysteria-public-domain" name="public_domain" readonly required autocomplete="off">
        </div>
          <label>
            UDP порт *
            <input name="hysteria_port" type="number" min="1" max="65535" value="443" required>
          </label>
        <div class="notice">Разрешите входящий TCP 80 для выпуска сертификата и входящий UDP-трафик на выбранный порт в firewall провайдера.</div>
      </fieldset>
      ${!isAttach ? window.LazHysteriaSettings.renderFields({settings:window.LazHysteriaSettings.defaults()},{install:true}) : ""}
      <div id="hysteria-install-progress"></div>
      <div class="laz-modal-actions">
        <button type="button" data-close-dialog>Отмена</button>
        <button type="submit" class="primary">${isAttach ? "Подключить" : "Установить"}</button>
      </div>
    </form>
  `;
}

function openHysteriaNodeDialog(mode = "install") {
  const isAttach = mode === "attach";
  const dialog = window.lazUI.openModal({
    id: "hysteria-node-dialog",
    title: isAttach ? "Подключить Hysteria2" : "Установить Hysteria2",
    body: renderHysteriaNodeDialogBody(mode),
  });
  const form = dialog.querySelector("#hysteria-node-form");
  const initialSettings={config_version_ms:0,endpoint_host:"",endpoint_port:443,settings:window.LazHysteriaSettings.defaults()};
  if(!isAttach){form.noValidate=true;window.LazHysteriaSettings.bind(form,initialSettings,(body,signal)=>api("/api/v1/nodes/hysteria-files",{method:"POST",body,signal}))}
  const serverFields = window.lazServerFields.bind(form);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const submit = form.querySelector('[type="submit"]');
    if (submit.disabled) return;
    if(!isAttach&&!window.LazHysteriaSettings.validate(form,initialSettings))return;
    submit.disabled = true;
    if (!await serverFields.prepare()) {
      submit.disabled = false;
      return;
    }
    const data = new FormData(form);
    const progress = new window.LazInstallProgress(form.querySelector("#hysteria-install-progress"), [
      ["connect", "Подключение к серверу"],
      ["check_system", "Проверка системы"],
      ["create_user", "Создание SSH-пользователя LazGate"],
      ["install_or_detect", isAttach ? "Поиск существующей Hysteria2" : "Установка Hysteria2"],
      ["write_config", "Запись конфигурации Hysteria2"],
      ["register_node", "Регистрация сервера"],
      ["install_agent", "Установка LazGate Agent"],
      ["start_service", "Запуск сервиса Hysteria2"],
      ["verify", "Проверка сервиса"],
      ["wait_agent", "Ожидание LazGate Agent"],
      ["done", "Готово"],
    ].map(([type, name]) => ({ type: "hysteria.install." + type, name })), state.webPrefix);
    state.installProgress = progress;
    try {
      const result = await api(isAttach ? "/api/v1/nodes/attach-hysteria2" : "/api/v1/nodes/install-hysteria2", {
        method: "POST",
        body: {
          ssh_host: String(data.get("ssh_host") || "").trim(),
          ssh_port: Number(data.get("ssh_port") || 22),
          bootstrap_user: String(data.get("bootstrap_user") || "root").trim(),
          bootstrap_password: String(data.get("bootstrap_password") || ""),
          node_name: String(data.get("node_name") || "").trim(),
          region: String(data.get("region") || ""),
          public_domain: String(data.get("public_domain") || "").trim(),
          hysteria_port: Number(data.get("hysteria_port") || 443),
          ...(!isAttach?{settings:window.LazHysteriaSettings.read(form,initialSettings).settings,file_set_id:window.LazHysteriaSettings.read(form,initialSettings).file_set_id}:{}),
        },
      });
      progress.accept(result);
      form.elements.bootstrap_password.value = "";
      if(form.querySelector('[data-h2-file-status]'))form.querySelector('[data-h2-file-status]').hidden=true;
      form.querySelectorAll("input, select, textarea, [data-h2-settings] button").forEach((input) => { input.disabled = true; });
      submit.hidden = true;
      form.querySelector("[data-close-dialog]").textContent = "Закрыть";
      showToast("Установка запланирована");
    } catch (error) {
      progress.fail(error.message || String(error));
      submit.disabled = false;
      showToast(error.message || "Установка не удалась");
    }
  });
}

function renderNodePage(details) {
  const node = details.node || {};
  const runtime = details.runtime || {};
  const totals = details.totals || {};
  app.innerHTML = `
    <section class="page-head">
      <div>
        <h1 class="page-title">Сервер ${escapeHTML(node.name || node.id || "")}</h1>
        <p class="subline">
          ${escapeHTML(nodeTypeLabel(node.type))} · ${escapeHTML(node.region || "Регион не указан")} · ${renderStatus(node.status || "active")}
        </p>
      </div>
      <div class="toolbar nav-toolbar">
        <button id="reload" type="button">Обновить</button>
        <a href="${state.webPrefix}/nodes" data-link><button type="button">Назад</button></a>
      </div>
    </section>
    <section class="node-grid">
      <article class="node-card">
        <h2>Агент</h2>
        <dl class="node-facts">
          <div><dt>Состояние VPN</dt><dd>${escapeHTML(nodeRuntimeStatusLabel(runtime.service_status))}</dd></div>
          <div><dt>Статус</dt><dd>${escapeHTML(nodeRuntimeStatusLabel(runtime.agent_status))}</dd></div>
          <div><dt>Последний сигнал</dt><dd>${formatDateTime(runtime.last_heartbeat_at)}</dd></div>
          <div><dt>Агент</dt><dd>${escapeHTML([runtime.protocol_version, runtime.agent_version].filter(Boolean).join("/") || "—")}</dd></div>
        </dl>
      </article>
      <article class="node-card">
        <h2>Трафик за сутки</h2>
        <dl class="node-facts">
          <div><dt>Активные сессии</dt><dd>${escapeHTML(String(totals.online_count || 0))}</dd></div>
          <div><dt>Отправлено сервером</dt><dd>${formatBytes(totals.tx_bytes || 0)}</dd></div>
          <div><dt>Получено сервером</dt><dd>${formatBytes(totals.rx_bytes || 0)}</dd></div>
          <div><dt>Всего</dt><dd>${formatBytes(totals.total_bytes || 0)}</dd></div>
        </dl>
      </article>
    </section>
    <section class="toolbar action-toolbar">
      ${renderNodeRuntimeActions(node)}
      ${node.type === "native_hysteria" && node.status !== "deleted" ? `<button type="button" id="detach-hysteria2">Удалить подключение Hysteria2 VPS</button><button type="button" id="uninstall-hysteria2" class="danger">Удалить Hysteria2 VPS</button>` : ""}
    </section>
    <section class="node-card">
      <h2>Активные подключения</h2>
      ${renderNodeOnline(details.online || [])}
    </section>
    <section class="node-card">
      <h2>Последние отчёты о трафике</h2>
      ${renderNodeUsage(details.usage || [])}
    </section>
    <section class="node-card">
      <h2>Команды</h2>
      ${renderNodeCommands(details.commands || [])}
    </section>
  `;
  document.querySelector("#reload").addEventListener("click", load);
  app.querySelectorAll(".node-card table").forEach((table) => {
    const headers = [...table.querySelectorAll("thead th")].map((cell) => cell.textContent);
    table.querySelectorAll("tbody tr").forEach((row) => {
      [...row.children].forEach((cell, index) => { cell.dataset.label = headers[index] || ""; });
    });
  });
  app.querySelectorAll("[data-node-command]").forEach((button) => {
    button.addEventListener("click", () => createNodeCommand(node.id, button.dataset.nodeCommand));
  });
  const editAmneziaButton = app.querySelector("#edit-amnezia-settings");
  app.querySelector("#edit-hysteria-settings")?.addEventListener("click",async()=>{
    try{const path=`/api/v1/nodes/${encodeURIComponent(node.id)}/hysteria-settings`;const settings=await api(path);window.LazHysteriaSettings.open(settings,node.name||"Hysteria",body=>api(path,{method:"PUT",body}),()=>api(path),(body,signal)=>api("/api/v1/nodes/hysteria-files",{method:"POST",body,signal}))}catch(e){showToast(e.message)}
  });
  if (editAmneziaButton) {
    editAmneziaButton.addEventListener("click", () => {
      if (!details.amnezia_settings) {
        showToast("Настройки сервера недоступны");
        return;
      }
      window.LazAmneziaSettings.open(details.amnezia_settings, node.name || "AmneziaWG", (body, signal) =>
        api("/api/v1/nodes/amneziawg-signatures", { method: "POST", body, signal }),
        (body) => api(`/api/v1/nodes/${encodeURIComponent(node.id)}/amnezia-settings`, {method:"PUT",body}),
        () => api(`/api/v1/nodes/${encodeURIComponent(node.id)}/amnezia-settings`));
    });
  }
  const detachButton = document.querySelector("#detach-hysteria2");
  if (detachButton) {
    detachButton.addEventListener("click", () => openConfirmDialog({
      title: "Удалить подключение Hysteria2 VPS?",
      message: "Удалить подключение сервера Hysteria2 к LazGate и восстановить исходные auth и trafficStats? Это действие необратимо.",
      confirmLabel: "Удалить подключение Hysteria2 VPS",
      onConfirm: async () => {
        await detachHysteria2Node(node.id);
      },
    }));
  }
  const uninstallButton = document.querySelector("#uninstall-hysteria2");
  if (uninstallButton) {
    uninstallButton.addEventListener("click", () => openConfirmDialog({
      title: "Удалить Hysteria2 VPS?",
      message: "Полностью удалить сервер Hysteria2? Это действие необратимо.",
      confirmLabel: "Удалить Hysteria2 VPS",
      onConfirm: async () => {
        await uninstallHysteria2Node(node.id);
      },
    }));
  }
}

function nodeRuntimeStatusLabel(value) {
  return ({ online: "На связи", offline: "Нет связи", degraded: "Работает с проблемами", active: "Активен", running: "Активен", inactive: "Остановлен", stopped: "Остановлен", failed: "Ошибка", unknown: "Неизвестно" })[value] || value || "Неизвестно";
}

function renderNodeRuntimeActions(node) {
  if (node.type === NATIVE_AMNEZIAWG_NODE_TYPE) {
    return `
      <button type="button" data-node-command="GetAmneziaWGStatus">Проверить состояние</button>
      <button type="button" data-node-command="CollectAmneziaWGLogs">Получить журналы</button>
      <button type="button" data-node-command="RestartAmneziaWG">Перезапустить</button>
      ${node.status === "deleted" ? "" : '<button type="button" id="edit-amnezia-settings">Редактировать</button>'}
    `;
  }
  return `
    <button type="button" data-node-command="GetStatus">Проверить состояние</button>
    <button type="button" data-node-command="RestartHysteria">Перезапустить</button>
    <button type="button" data-node-command="StartHysteria">Запустить</button>
    <button type="button" data-node-command="StopHysteria" class="danger">Остановить</button>
    <button type="button" data-node-command="CollectLogs">Получить журналы</button>
    <button type="button" data-node-command="DumpStreams">Получить список потоков</button>
    ${node.status === "deleted" ? "" : '<button type="button" id="edit-hysteria-settings">Редактировать</button>'}
  `;
}

function renderNodeOnline(items) {
  if (items.length === 0) {
    return `<div class="empty inline-empty">Нет активных подключений.</div>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>ConnectionID</th><th>Количество сессий</th><th>Впервые обнаружено</th><th>Последняя активность</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td class="mono">${escapeHTML(item.connection_id || "")}</td>
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
    return `<div class="empty inline-empty">Отчётов о трафике пока нет.</div>`;
  }
  return `
    <div class="table-wrap compact-table">
      <table>
        <thead><tr><th>Пользователь</th><th>Подключение</th><th>ConnectionID</th><th>Отправлено сервером</th><th>Получено сервером</th><th>Всего</th><th>Интервал сбора</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td>${escapeHTML(item.account_name || "Неизвестный пользователь")}</td>
              <td>${escapeHTML(item.connection_name || "Неизвестное подключение")}</td>
              <td class="mono">${escapeHTML(item.connection_id || "")}</td>
              <td>${formatBytes(item.tx_bytes || 0)}</td>
              <td>${formatBytes(item.rx_bytes || 0)}</td>
              <td>${formatBytes(item.total_bytes || 0)}</td>
              <td>${formatMSDate(item.from_ms)} - ${formatMSDate(item.to_ms)}</td>
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
        <thead><tr><th>Команда</th><th>Статус</th><th>Создана</th><th>Результат</th></tr></thead>
        <tbody>
          ${items.map((item) => `
            <tr>
              <td>${escapeHTML(nodeCommandLabel(item.Type || item.type || ""))}</td>
              <td>${renderCommandStatus(item.Status || item.status)}</td>
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

function renderCommandStatus(status) {
  const label = ({ active: "Ожидает результата", completed: "Завершена", error: "Ошибка" })[status] || "Неизвестно";
  const tone = ({ active: "held", completed: "active", error: "error" })[status] || "";
  return `<span class="status ${tone}">${label}</span>`;
}

function nodeCommandLabel(type) {
  return ({ GetStatus: "Проверить состояние", GetAmneziaWGStatus: "Проверить состояние", GetAWGStatus: "Проверить состояние", RestartHysteria: "Перезапустить", RestartAmneziaWG: "Перезапустить", RestartAWG: "Перезапустить", StartHysteria: "Запустить", StopHysteria: "Остановить", CollectLogs: "Получить журналы", CollectAmneziaWGLogs: "Получить журналы", CollectAWGLogs: "Получить журналы", DumpStreams: "Получить список потоков", ApplyHysteriaConfig: "Применить настройки" })[type] || type;
}

async function createNodeCommand(nodeID, type) {
  await api(`/api/v1/nodes/${encodeURIComponent(nodeID)}/commands`, {
    method: "POST",
    body: { type, payload: {}, expires_in_seconds: 300 },
  });
  showToast("Команда отправлена агенту");
  await load();
}

async function detachHysteria2Node(nodeID) {
  await api(`/api/v1/nodes/${encodeURIComponent(nodeID)}/detach-hysteria2`, {
    method: "POST",
  });
  showToast("Удаление подключения Hysteria2 VPS запланировано");
  await load();
}

async function uninstallHysteria2Node(nodeID) {
  await api(`/api/v1/nodes/${encodeURIComponent(nodeID)}/uninstall-hysteria2`, {
    method: "POST",
  });
  showToast("Удаление Hysteria2 VPS запланировано");
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
    const node = item.node || {};
    return `
      <tr>
        <td data-label="Подключение">${escapeHTML(connection.name || connection.id || "Подключение")}<div class="muted mono">${escapeHTML(connection.id || "")}</div></td>
        <td data-label="Нода">
          ${escapeHTML(node.name || connection.node_id || "Сервер")}
          <div class="muted">${escapeHTML(nodeTypeLabel(node.type))}</div>
        </td>
        <td data-label="Тип">${protocolLabel(connection.protocol)}</td>
        <td data-label="Состояние">${renderConnectionState(connection)}</td>
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
            <th>Подключение</th>
            <th>Нода</th>
            <th>Тип</th>
            <th>Состояние</th>
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
        <td data-label="Подключения">${renderConnectionSummary(summary.connections || [])}</td>
        <td data-label="Статус">${renderStatus(account.status)}</td>
        <td data-label="Создано">${formatDateTime(account.created_at)}</td>
        <td data-label="Действия">
          ${state.showDeleted ? `<span class="muted">Только просмотр</span>` : `<div class="inline-actions">
            <button type="button" data-copy-page="${escapeAttr(account.id)}">Страница конфигов</button>
            <button type="button" data-add-connection="${escapeAttr(account.id)}">Добавить подключение</button>
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
            <th>Подключения</th>
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
  const isDeletedUser = summary.account.status === "deleted";
  if (connections.length === 0) {
    return `<section class="empty">Подключений пока нет.</section>`;
  }
  const rows = connections.map((item) => {
    const connection = item.connection;
    return `
      <tr>
        <td data-label="Подключение">
          ${escapeHTML(connection.name || connection.id)}
          <div class="muted mono">${escapeHTML(connection.id)}</div>
          <div class="muted">${escapeHTML(item.node?.name || connection.node_id)}</div>
        </td>
        <td data-label="Тип">${protocolLabel(connection.protocol)}</td>
        <td data-label="Статус">${renderConnectionState(connection)}</td>
        <td data-label="Создано">${formatDateTime(connection.created_at)}</td>
        ${isDeletedUser ? `
          <td data-label="Удалено">${connection.desired_status === "deleted" ? formatDateTime(connection.updated_at) : `<span class="muted">-</span>`}</td>
          <td data-label="Действия">
            ${connection.desired_status === "deleted"
              ? `<span class="muted">Удалено</span>`
              : `<button type="button" class="danger" data-delete-connection="${escapeAttr(connection.id)}">Удалить</button>`}
          </td>
        ` : `
          <td data-label="Действия">
            <div class="inline-actions">
              ${connection.desired_status === "held"
                ? `<button type="button" data-resume-connection="${escapeAttr(connection.id)}">Разблокировать</button>`
                : `<button type="button" data-hold-connection="${escapeAttr(connection.id)}">Заблокировать</button>`}
              <button type="button" class="danger" data-delete-connection="${escapeAttr(connection.id)}">Удалить</button>
            </div>
          </td>
        `}
      </tr>
    `;
  }).join("");
  return `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
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
  app.querySelectorAll("[data-add-connection]").forEach((button) => {
    button.addEventListener("click", async () => {
      history.pushState(null, "", `${state.webPrefix}/accounts/${encodeURIComponent(button.dataset.addConnection)}`);
      await load();
      openConnectionDialog();
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
}

function openConfirmDialog({ title, message, confirmLabel = "Подтвердить", onConfirm, onCancel }) {
  let confirmed = false;
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
  dialog.addEventListener("close", () => {
    if (!confirmed) {
      onCancel?.();
    }
  });
  dialog.querySelector("#confirm-action").addEventListener("click", async () => {
    confirmed = true;
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
  const links = [
    ["config_page", "Страница конфигов", enrollment.config_page],
  ].filter(([, , value]) => value);
  const errors = (enrollment.results || []).filter((item) => item.status === "error");
  return `
    <section class="provision-summary">
      <div>
        <strong>${escapeHTML(account.display_name || account.username || "Аккаунт")}</strong>
        <div class="muted">${escapeHTML(account.username || "")}</div>
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
                <strong>${escapeHTML(item.node?.name || item.node?.id || "Сервер")}</strong>
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

async function copyAccountTokenURL(accountID, field) {
  await copyText(await accountTokenURL(accountID, field));
}

async function accountTokenURL(accountID, field) {
  const payload = await api("/api/v1/tokens", {
    method: "POST",
    body: { scope: "portal", account_id: accountID },
  });
  return payload[field] || "";
}

async function showConfigPageQR(accountID) {
  state.qrDialogValue = await accountTokenURL(accountID, "config_page");
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

async function provisionSelectedNodes(connectionName, nodeIDs) {
  for (const nodeID of nodeIDs) {
    const node = state.nodes.find((item) => item.id === nodeID);
    await api("/api/v1/connections/provision", {
      method: "POST",
      body: {
        account_id: state.currentSummary.account.id,
        node_id: nodeID,
        name: connectionName,
        protocol: protocolForNode(node),
      },
    });
  }
}

function openConnectionDialog() {
  if (!state.currentSummary) {
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "connection-dialog",
    title: "Добавить подключение",
    body: `
      <form id="connection-form" method="dialog" class="laz-modal-fields">
        <label>
          Название
          <input name="name" required autocomplete="off" value="Подключение ${(state.currentSummary.connections || []).length + 1}">
        </label>
        <fieldset>
        <legend>Серверы</legend>
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
  fillNodeChecks("connection-node-list");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const name = String(data.get("name") || "").trim();
    const nodeIDs = checkedNodeIDs("connection-node-list");
    if (!name) {
      showToast("Введите название подключения");
      return;
    }
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы один сервер");
      return;
    }
    await provisionSelectedNodes(name, nodeIDs);
    dialog.close();
    showToast("Подключение добавлено");
    await load();
  });
}

function fillNodeChecks(targetID, excludeNodeIDs = new Set()) {
  const target = document.querySelector(`#${targetID}`);
  const supported = state.nodes.filter((node) => protocolForNode(node) && !excludeNodeIDs.has(node.id));
  if (supported.length === 0) {
    target.innerHTML = excludeNodeIDs.size > 0
      ? `<div class="muted">Выбранные серверы недоступны.</div>`
      : `<div class="muted">Нет поддерживаемых активных серверов.</div>`;
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

function adminAPIPath(path) {
  if (path.startsWith("/api/v1")) {
    return `${apiBase}${path.slice("/api/v1".length)}`;
  }
  return path;
}

async function api(path, options = {}) {
  const init = {
    method: options.method || "GET",
    signal: options.signal,
    credentials: "same-origin",
    headers: {
      ...csrfHeaders(),
    },
  };
  if (options.body !== undefined) {
    if(options.body instanceof FormData){init.body=options.body}else{init.headers["Content-Type"] = "application/json";init.body = JSON.stringify(options.body)}
  }
  const response = await fetch(adminAPIPath(path), init);
  const contentType = response.headers.get("content-type") || "";
  const data = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = typeof data === "object" && data.error ? data.error : `HTTP ${response.status}`;
    if (response.status === 401) {
      expireAdminSession();
      return null;
    }
    const error = new Error(message);
    error.status = response.status;
    error.payload = data;
    throw error;
  }
  applyAuthTimings(data);
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

function currentAdministrationSection() {
  const path = location.pathname.replace(/\/$/, "");
  if (path === `${state.webPrefix}/administration`) {
    history.replaceState(null, "", `${state.webPrefix}/administration/invites`);
    return "invites";
  }
  if (path === `${state.webPrefix}/administration/invites`) {
    return "invites";
  }
  if (path === `${state.webPrefix}/administration/admins`) {
    return "admins";
  }
  document.body.classList.remove("admin-mode");
  setAdminNavActiveSection("");
  return "";
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

function renderConnectionSummary(connections) {
  if (connections.length === 0) {
    return `<span class="muted">Нет подключений</span>`;
  }
  const names = connections.slice(0, 3).map((item) => escapeHTML(item.connection?.name || item.connection?.id || "Подключение"));
  const more = connections.length > 3 ? ` ... (${connections.length})` : "";
  return `${names.join(", ")}${more}`;
}

function renderConnectionState(connection) {
  const desired = connection.desired_status || "unknown";
  const applied = connection.applied_status || "unknown";
  const pending = Boolean(connection.pending) || desired !== applied;
  if (!pending) {
    return renderStatus(applied);
  }
  return `${renderStatus(desired)} <span class="muted">ожидает применения</span>`;
}

function renderNodePills(nodes) {
  if (nodes.length === 0) {
    return `<span class="muted">Нет серверов</span>`;
  }
  return nodes.map((node) => `
    <a class="node-pill" href="${state.webPrefix}/nodes/${encodeURIComponent(node.id)}" data-link>
      <span>${escapeHTML(node.name)}</span>
      <span class="muted">(${nodeTypeLabel(node.type)})</span>
      <span class="status ${escapeAttr(node.agent_status === "online" ? "active" : "held")}">${escapeHTML(node.agent_status || "offline")}</span>
      <span class="muted">${escapeHTML(node.service_status || "-")}</span>
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
    case AMNEZIA_NODE_TYPE:
      return "AmneziaWG API";
    case NATIVE_AMNEZIAWG_NODE_TYPE:
      return "AmneziaWG";
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
    case AMNEZIA_NODE_TYPE:
    case NATIVE_AMNEZIAWG_NODE_TYPE:
      return "amneziawg";
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
    case "used":
      return "Использовано";
    case "revoked":
      return "Отозвано";
    case "expired":
      return "Истекло";
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
    return "—";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }
  return escapeHTML(date.toLocaleString());
}

function formatMSDate(value) {
  const ms = Number(value || 0);
  if (!ms) {
    return "—";
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
  let minutesTotal = Math.floor(Number(ms || 0) / 60000);
  if (minutesTotal <= 0) {
    return "0м";
  }
  const days = Math.floor(minutesTotal / 1440);
  minutesTotal %= 1440;
  const hours = Math.floor(minutesTotal / 60);
  const minutes = minutesTotal % 60;
  const parts = [];
  if (days) parts.push(`${days}д`);
  if (hours) parts.push(`${hours}ч`);
  if (minutes) parts.push(`${minutes}м`);
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
    const response = await fetch(adminAPIPath(path), {
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
