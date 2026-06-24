const MAX_EVENT_HISTORY = 100;

const data = JSON.parse(document.querySelector("#connect-data").textContent || "{}");
let summary = null;
let selectedApp = "";
let selectedConnectionID = "";
let selectedHPProfile = "";
const app = document.querySelector("#connect-app");
const pageTitle = document.querySelector("#connect-title");
const userLine = document.querySelector("#connect-account");
const toast = document.querySelector("#toast");
const eventHistoryLink = document.querySelector("#event-history-link");
const eventHistoryCount = document.querySelector("#event-history-count");
const qrReferences = new Map();
let unlockChallenge = "";
let unlockChallengeState = null;
let unlockChallengeDifficulty = 18;
const sessionKey = "laz_client_session";
let sessionToken = localStorage.getItem(sessionKey) || "";
let strongCSRFToken = "";
let recoveryCode = "";
let liteCSRFToken = "";
let eventSource = null;
let eventReloadTimer = 0;
let refreshTimer = 0;
let refreshAfterMS = 0;
const eventHistory = [];

if (eventHistoryLink) {
  eventHistoryLink.addEventListener("click", openEventHistory);
}

document.addEventListener("visibilitychange", handleVisibilityChange);
window.addEventListener("pagehide", () => {
  stopRefreshTimer();
  stopClientEvents();
});
window.addEventListener("pageshow", handleVisiblePage);

setPageHeader();
userLine.innerHTML = sessionToken ? `<span class="subline">Войти по сохраненной сессии</span>` : `<span class="subline">Введите проверочный код</span>`;
if (sessionToken) {
  loadSession();
} else {
  renderUnlock();
}

async function renderUnlock() {
  unlockChallengeState = null;
  setPageHeader();
  userLine.innerHTML = `<span class="subline">Введите проверочный код</span>`;
  app.innerHTML = `
    <article class="connect-card">
      <div>
        <h2>Доступ</h2>
        <p id="access-instructions" class="subline" role="status">Загружаем проверку…</p>
      </div>
      ${renderLinkUnlockForm()}
    </article>
  `;
  const unlockForm = app.querySelector("#unlock-form");
  if (unlockForm) {
    const input = unlockForm.elements.access_check;
    const submit = unlockForm.querySelector('[type="submit"]');
    input.disabled = true;
    submit.disabled = true;
    unlockForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const form = new FormData(event.currentTarget);
      await unlock(String(form.get("access_check") || ""));
    });
    try {
      const check = await fetchUnlockChallenge();
      if (!unlockForm.isConnected) return;
      unlockChallengeState = check.challenge;
      unlockChallengeDifficulty = check.pow_difficulty || 18;
      app.querySelector("#access-instructions").textContent =
        "Введите символы имени аккаунта на позициях " +
        check.challenge.positions.map((position) => position + 1).join(", ") +
        " в указанном порядке. Считайте с первого символа.";
      input.disabled = false;
      submit.disabled = false;
      input.focus();
    } catch (error) {
      if (!unlockForm.isConnected) return;
      app.querySelector("#access-instructions").textContent = error.message || "Не удалось получить проверку.";
      submit.disabled = false;
      submit.type = "button";
      submit.textContent = "Повторить";
      submit.addEventListener("click", renderUnlock, { once: true });
    }
  }
}

function renderLinkUnlockForm() {
  return `
      <form id="unlock-form" class="unlock-form" autocomplete="off">
        <label>
          Код
          <input name="access_check" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="16">
        </label>
        <div class="connect-actions">
          <button type="submit" class="primary">Открыть</button>
        </div>
      </form>
  `;
}

function renderLoginForm(id = "login-form", secondaryAction = "") {
  return `
    <form id="${escapeAttr(id)}" class="unlock-form">
      <label>
        Пароль
        <input name="password" type="password" required autocomplete="current-password">
      </label>
      <div class="connect-actions">
        <button type="submit" class="primary">Войти</button>
        ${secondaryAction}
      </div>
    </form>
  `;
}

function renderRecoveryForm(id = "recovery-form", secondaryAction = "") {
  return `
    <form id="${escapeAttr(id)}" class="unlock-form">
      <label>
        Recovery code
        <input name="recovery_code" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false">
      </label>
      <label>
        Новый пароль
        <input name="new_password" type="password" required autocomplete="new-password">
      </label>
      <div class="connect-actions">
        <button type="submit" class="primary">Сбросить пароль</button>
        ${secondaryAction}
      </div>
    </form>
  `;
}

async function fetchUnlockChallenge() {
  const challengeResponse = await fetch("/client/v1/configs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token: data.token || "" }),
  });
  const challengePayload = await challengeResponse.json().catch(() => ({}));
  if (!challengeResponse.ok || !Array.isArray(challengePayload.challenge?.positions) ||
      challengePayload.challenge.positions.length !== 4 ||
      !challengePayload.challenge.positions.every((position) => Number.isInteger(position) && position >= 0)) {
    throw new Error(challengePayload.error || "Не удалось получить проверку");
  }
  return challengePayload;
}

async function unlock(answer) {
  const form = app.querySelector("#unlock-form");
  const submit = form?.querySelector('[type="submit"]');
  if (submit) submit.disabled = true;
  try {
    if (!unlockChallengeState || unlockChallengeState.expires_at <= Math.floor(Date.now() / 1000)) {
      const check = await fetchUnlockChallenge();
      unlockChallengeState = check.challenge;
      unlockChallengeDifficulty = check.pow_difficulty || 18;
    }
    const powNonce = await solvePoW(unlockChallengeState.signature, unlockChallengeDifficulty);
    const response = await fetch("/client/v1/configs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({
        token: data.token || "",
        answer,
        pow_nonce: powNonce,
        challenge_state: unlockChallengeState,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось открыть");
      return;
    }
    liteCSRFToken = payload.csrf_token || "";
    unlockChallenge = answer;
    summary = payload || {};
    setPageHeader(summary);
    userLine.innerHTML = "";
    startClientEvents();
    render();
  } catch (error) {
    showToast(error.message || String(error));
  } finally {
    if (submit) submit.disabled = false;
  }
}

async function setupPassword(password) {
  try {
    const response = await fetch("/client/v1/password/setup", {
      method: "POST",
      headers: liteHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({
        token: data.token || "",
        password,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось задать пароль");
      return false;
    }
    recoveryCode = payload.recovery_code || "";
    strongCSRFToken = payload.csrf_token || "";
    sessionToken = payload.session_token || "cookie";
    localStorage.setItem(sessionKey, sessionToken);
    applyStrongAuthTimings(payload);
    if (summary) {
      summary.pin_enabled = true;
    }
    showToast("Пароль задан");
    renderRecoveryResult();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function login(password) {
  try {
    const response = await fetch("/client/v1/login", {
      method: "POST",
      headers: liteHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({
        token: data.token || "",
        password,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось войти");
      return false;
    }
    sessionToken = payload.session_token || "cookie";
    strongCSRFToken = payload.csrf_token || "";
    localStorage.setItem(sessionKey, sessionToken);
    applyStrongAuthTimings(payload);
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function recoverPassword(recovery_code, new_password) {
  try {
    const response = await fetch("/client/v1/recover", {
      method: "POST",
      headers: liteHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({
        token: data.token || "",
        recovery_code,
        new_password,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось восстановить");
      return false;
    }
    recoveryCode = payload.recovery_code || "";
    strongCSRFToken = payload.csrf_token || "";
    sessionToken = payload.session_token || sessionToken || "cookie";
    localStorage.setItem(sessionKey, sessionToken);
    applyStrongAuthTimings(payload);
    showToast("Пароль обновлен");
    renderRecoveryResult();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function loadSession() {
  try {
    const response = await fetch("/client/v1/session/configs", {
      headers: strongHeaders(),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      expireStrongSession(payload.error || "Сессия недействительна");
      return;
    }
    unlockChallenge = "";
    summary = payload || {};
    setPageHeader(summary);
    userLine.innerHTML = "";
    startClientEvents();
    render();
  } catch (error) {
    showToast(error.message || String(error));
  }
}

function startClientEvents() {
  stopClientEvents();
  if (!sessionToken || document.visibilityState === "hidden") {
    return;
  }
  const source = new EventSource("/client/v1/events");
  eventSource = source;
  source.onmessage = handleClientEvent;
  ["connection.created", "connection.applied", "connection.deleted"].forEach((type) => {
    source.addEventListener(type, handleClientEvent);
  });
  source.onerror = () => {
    // EventSource reconnects itself.
  };
}

function stopClientEvents() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
}

function handleVisibilityChange() {
  if (document.visibilityState === "hidden") {
    stopRefreshTimer();
    stopClientEvents();
    return;
  }
  handleVisiblePage();
}

async function handleVisiblePage() {
  if (!sessionToken || document.visibilityState === "hidden") {
    return;
  }
  await refreshStrongAuthIfDue();
  scheduleStrongRefresh();
  startClientEvents();
}

function applyStrongAuthTimings(payload) {
  if (payload?.csrf_token) {
    strongCSRFToken = payload.csrf_token;
  }
  if (payload?.refresh_after) {
    refreshAfterMS = Date.parse(payload.refresh_after);
  }
  scheduleStrongRefresh();
}

function expireStrongSession(message) {
  localStorage.removeItem(sessionKey);
  sessionToken = "";
  strongCSRFToken = "";
  refreshAfterMS = 0;
  stopRefreshTimer();
  stopClientEvents();
  window.lazUI.closeModal?.();
  if (message) {
    showToast(message);
  }
  renderUnlock();
}

function scheduleStrongRefresh() {
  stopRefreshTimer();
  if (!sessionToken || !refreshAfterMS || document.visibilityState === "hidden") {
    return;
  }
  const delay = Math.max(0, refreshAfterMS - Date.now());
  refreshTimer = window.setTimeout(() => {
    refreshTimer = 0;
    refreshStrongAuth();
  }, delay);
}

function stopRefreshTimer() {
  if (refreshTimer) {
    window.clearTimeout(refreshTimer);
    refreshTimer = 0;
  }
}

async function refreshStrongAuthIfDue() {
  if (!refreshAfterMS || Date.now() < refreshAfterMS) {
    return;
  }
  await refreshStrongAuth();
}

async function refreshStrongAuth() {
  try {
    const response = await fetch("/client/v1/auth/refresh", {
      method: "POST",
      headers: strongHeaders(),
      credentials: "same-origin",
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      expireStrongSession("Сессия истекла");
      return;
    }
    applyStrongAuthTimings(payload);
  } catch (_) {
    scheduleStrongRefresh();
  }
}

function handleClientEvent(event) {
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
  window.clearTimeout(eventReloadTimer);
  eventReloadTimer = window.setTimeout(async () => {
    if (sessionToken) {
      await loadSession();
    } else if (unlockChallenge) {
      await unlock(unlockChallenge);
    }
  }, 400);
}

function recordDisplayedEvent(payload) {
  eventHistory.push({
    time: new Date().toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
    type: String(payload.type || ""),
    message: String(payload.message || ""),
  });
  if (eventHistory.length > MAX_EVENT_HISTORY) {
    eventHistory.splice(0, eventHistory.length - MAX_EVENT_HISTORY);
  }
  renderEventHistoryLink();
}

function renderEventHistoryLink() {
  if (!eventHistoryLink || !eventHistoryCount) {
    return;
  }
  eventHistoryCount.textContent = String(eventHistory.length);
  eventHistoryLink.disabled = eventHistory.length === 0;
}

function openEventHistory() {
  const items = eventHistory.slice().reverse();
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

function renderRecoveryResult() {
  userLine.innerHTML = "";
  app.innerHTML = `
    <article class="connect-card">
      <h2>Recovery code</h2>
      <p class="subline">Сохраните новый код восстановления. Старый код больше не работает.</p>
      <pre>${escapeHTML(recoveryCode)}</pre>
      <div class="connect-actions">
        <button type="button" class="primary" id="back-to-configs">Назад</button>
        <button type="button" id="copy-recovery-code">Скопировать</button>
      </div>
    </article>
  `;
  app.querySelector("#back-to-configs").addEventListener("click", render);
  app.querySelector("#copy-recovery-code").addEventListener("click", () => copyText(recoveryCode));
}

function render() {
  if (!summary) {
    renderUnlock();
    return;
  }
  const connections = availableConnections();
  if (!selectedConnectionID || !connections.some((item) => item.connection.id === selectedConnectionID)) {
    selectedConnectionID = connections[0]?.connection.id || "";
  }
  const selectedConnection = connections.find((item) => item.connection.id === selectedConnectionID);
  const configs = configsForConnection(selectedConnectionID);
  const amneziaConfigs = configs.filter(isAmneziaConfig);
  const hasHysteria = selectedConnection?.connection?.protocol === "hysteria2";
  if (!selectedApp || (selectedApp === "amnezia" && amneziaConfigs.length === 0) || (selectedApp === "hp" && !hasHysteria)) {
    selectedApp = amneziaConfigs.length > 0 ? "amnezia" : "hp";
  }
  const profiles = hpProfiles();
  if (selectedHPProfile && !profiles.some((profile) => profile.slug === selectedHPProfile)) {
    selectedHPProfile = "";
  }
  renderClientHeaderActions();
  qrReferences.clear();
  app.innerHTML = `
    ${renderSessionTools()}
    ${connections.length === 0 ? `<section class="empty">Нет активных подключений.</section>` : ""}
    ${renderConnectionContext(connections, amneziaConfigs.length > 0, hasHysteria)}
    ${connections.length > 0 && amneziaConfigs.length === 0 && !hasHysteria ? `<section class="empty">Нет активных конфигураций для выбранного подключения.</section>` : ""}
    ${selectedApp === "amnezia" ? renderAmneziaConfigs(amneziaConfigs) : ""}
    ${selectedApp === "hp" ? renderHPConfigs(hasHysteria) : ""}
  `;
  window.lazUI.bindDialogClose(app);
  app.querySelectorAll("[data-select-connection]").forEach((button) => {
    button.addEventListener("click", () => {
      selectedConnectionID = button.dataset.selectConnection;
      selectedApp = "";
      selectedHPProfile = "";
      render();
    });
  });
  app.querySelectorAll("[data-select-app]").forEach((button) => {
    button.addEventListener("click", () => {
      selectedApp = button.dataset.selectApp;
      selectedHPProfile = "";
      render();
    });
  });
  app.querySelectorAll("[data-view-client-sub]").forEach((button) => {
    button.addEventListener("click", () => {
      const profile = button.dataset.viewClientSub || "";
      selectedHPProfile = selectedHPProfile === profile ? "" : profile;
      render();
    });
  });
  app.querySelectorAll("[data-copy]").forEach((button) => {
    button.addEventListener("click", () => {
      const cfg = amneziaConfigs.find((item) => item.id === button.dataset.copy);
      copyText(cfg?.import_url || cfg?.config || "");
    });
  });
  app.querySelectorAll("[data-download-config]").forEach((button) => {
    button.addEventListener("click", () => {
      const cfg = amneziaConfigs.find((item) => item.id === button.dataset.downloadConfig);
      if (!cfg) return;
      const url = URL.createObjectURL(new Blob([cfg.config], { type: "text/plain;charset=utf-8" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = "amneziawg.conf";
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    });
  });
  app.querySelectorAll("[data-copy-client-sub]").forEach((button) => {
    button.addEventListener("click", () => copyText(button.dataset.value || ""));
  });
  const openPasswordDialog = app.querySelector("#open-password-dialog");
  if (openPasswordDialog) {
    openPasswordDialog.addEventListener("click", openPasswordModal);
  }
  const logout = userLine.querySelector("#client-logout");
  if (logout) {
    logout.addEventListener("click", confirmLogout);
  }
  const rotateRecovery = userLine.querySelector("#rotate-recovery-code");
  if (rotateRecovery) {
    rotateRecovery.addEventListener("click", confirmRotateRecoveryCode);
  }
  const deleteConnectionButton = app.querySelector("#open-connection-delete");
  if (deleteConnectionButton) {
    deleteConnectionButton.addEventListener("click", openDeleteConnectionModal);
  }
  const copyRecovery = app.querySelector("#copy-recovery-code");
  if (copyRecovery) {
    copyRecovery.addEventListener("click", () => copyText(recoveryCode));
  }
  const openFullAccess = app.querySelector("#open-full-access");
  if (openFullAccess) {
    openFullAccess.addEventListener("click", openFullAccessModal);
  }
  const openConnection = app.querySelector("#open-connection-create");
  if (openConnection) {
    openConnection.addEventListener("click", openCreateConnectionModal);
  }
  renderQRCodes();
  loadSelectedConnectionSubscription();
}

function renderClientHeaderActions() {
  if (!sessionToken) {
    userLine.innerHTML = "";
    return;
  }
  userLine.innerHTML = `
    <button type="button" id="rotate-recovery-code">Сбросить recovery code</button>
    <button type="button" id="client-logout">Выйти</button>
  `;
}

function renderSessionTools() {
  const blocks = [];
  if (data.token && unlockChallenge && !sessionToken && !summary?.pin_enabled) {
    blocks.push(`
      <section class="access-strip">
        <span>Открыт базовый доступ. Для создания подключений нужен пароль.</span>
        <button type="button" id="open-password-dialog">Задать пароль</button>
      </section>
    `);
  }
  if (data.token && unlockChallenge && !sessionToken && summary?.pin_enabled) {
    blocks.push(`
      <section class="access-strip">
        <span>Открыт базовый доступ. Для полного доступа войдите по паролю.</span>
        <button type="button" id="open-full-access">Полный доступ</button>
      </section>
    `);
  }
  if (sessionToken) {
    blocks.push(`
      <section class="access-strip">
        <span>${escapeHTML(policyText())}</span>
        <div class="connect-actions">
          ${canCreateConnection() ? `<button type="button" id="open-connection-create">Создать подключение</button>` : ""}
          ${availableConnections().length > 0 ? `<button type="button" id="open-connection-delete">Удалить подключение</button>` : ""}
        </div>
      </section>
    `);
  }
  return blocks.join("");
}

function openCreateConnectionModal() {
  const next = availableConnections().length + 1;
  const dialog = window.lazUI.openModal({
    id: "connection-create-dialog",
    title: "Создать подключение",
    initialFocus: "[name='connection_name']",
    body: `
      <form id="connection-form" class="unlock-form">
        <label>
          Название
          <input name="connection_name" required autocomplete="off" value="${escapeAttr(`Подключение ${next}`)}">
        </label>
        <fieldset><legend>Серверы</legend><div id="connection-node-list" class="check-list"></div></fieldset>
        <div class="connect-actions">
          <button type="submit" class="primary">Создать подключение</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#connection-form");
  renderConnectionNodeChoices(dialog.querySelector("#connection-node-list"));
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const values = new FormData(form);
    const nodeIDs = [...form.querySelectorAll("input[name='node_id']:checked")].map((item) => item.value);
    if (nodeIDs.length === 0) {
      showToast("Выберите хотя бы один сервер");
      return;
    }
    if (await createConnections(String(values.get("connection_name") || ""), nodeIDs)) {
      dialog.close();
    }
  });
}

function renderConnectionNodeChoices(target) {
  const allowed = new Set(summary?.policy?.allowed_node_ids || []);
  const nodes = (summary?.nodes || []).filter((node) => node.status === "active" && allowed.has(node.id));
  target.innerHTML = nodes.length === 0
    ? `<span class="subline">Нет доступных серверов.</span>`
    : nodes.map((node) => `<label class="check-row"><input type="checkbox" name="node_id" value="${escapeAttr(node.id)}"><span>${escapeHTML(node.name)} <span class="subline">${escapeHTML(node.region || "")}</span></span></label>`).join("");
}

function openPasswordModal() {
  const dialog = window.lazUI.openModal({
    id: "password-dialog",
    title: "Пароль",
    initialFocus: "[name='password']",
    body: `
        <p class="subline">Пароль включает полный доступ: создание подключений и управление recovery code после повторного входа. Требования: минимум 6 символов, буква, цифра и символ.</p>
        ${recoveryCode ? `<pre>${escapeHTML(recoveryCode)}</pre>` : ""}
        <form id="password-form" class="unlock-form">
          <label>
            Новый пароль
            <input name="password" type="password" required autocomplete="new-password" minlength="6" pattern="(?=.*[A-Za-z])(?=.*\\d)(?=.*[^A-Za-z0-9]).{6,}" title="Минимум 6 символов: буква, цифра и символ">
          </label>
          <div class="connect-actions">
            <button type="submit" class="primary">Задать пароль</button>
            ${recoveryCode ? `<button type="button" id="copy-recovery-code">Скопировать recovery code</button>` : ""}
          </div>
        </form>
    `,
  });
  const form = dialog.querySelector("#password-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    if (await setupPassword(String(data.get("password") || ""))) {
      dialog.close();
    }
  });
}

function openFullAccessModal() {
  const dialog = window.lazUI.openModal({
    id: "full-access-dialog",
    title: "Полный доступ",
    initialFocus: "[name='password']",
    body: renderLoginForm("full-access-login-form", `<button type="button" id="show-recovery-form">Recovery</button>`),
  });
  dialog.querySelector("#full-access-login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    if (await login(String(form.get("password") || ""))) {
      dialog.close();
    }
  });
  dialog.querySelector("#show-recovery-form").addEventListener("click", () => {
    dialog.close();
    openRecoveryModal();
  });
}

function openRecoveryModal() {
  const dialog = window.lazUI.openModal({
    id: "full-access-recovery-dialog",
    title: "Смена пароля",
    initialFocus: "[name='recovery_code']",
    body: renderRecoveryForm("full-access-recovery-form", `<button type="button" id="show-login-form">Назад</button>`),
  });
  dialog.querySelector("#full-access-recovery-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    if (await recoverPassword(String(form.get("recovery_code") || ""), String(form.get("new_password") || ""))) {
      dialog.close();
    }
  });
  dialog.querySelector("#show-login-form").addEventListener("click", () => {
    dialog.close();
    openFullAccessModal();
  });
}

function renderConnectionContext(connections, hasAmnezia, hasHP) {
  if (connections.length === 0) {
    return "";
  }
  const options = availableAppOptions(hasAmnezia, hasHP);
  const protocolItems = protocolChoices(options);
  const appItems = options.map((option) => renderAppChoice(option.appLabel, option.key));
  const selectedOption = options.find((option) => option.key === selectedApp) || options[0];
  return `
    <section class="selection-panel">
      <div class="selection-row">
        <span class="context-label">Подключение</span>
        <div class="connect-actions">
          ${connections.map((item) => `
            <button type="button" class="app-choice ${selectedConnectionID === item.connection.id ? "active" : ""}" data-select-connection="${escapeAttr(item.connection.id)}">
              ${escapeHTML(item.connection.name || item.node?.name || "Подключение")}
            </button>
          `).join("")}
        </div>
      </div>
      ${options.length > 0 ? `
        <div class="selection-row">
          <span class="context-label">Протокол</span>
          <div class="connect-actions">${protocolItems.join("")}</div>
        </div>
        <div class="selection-row">
          <span class="context-label">Приложение</span>
          <div class="connect-actions">${appItems.join("")}</div>
        </div>
        ${selectedOption ? renderAppHelp(selectedOption) : ""}
      ` : ""}
    </section>
  `;
}

function availableAppOptions(hasAmnezia, hasHP) {
  const options = [];
  if (hasAmnezia) {
    options.push({
      key: "amnezia",
      protocol: "amneziawg",
      protocolLabel: "AmneziaWG",
      appLabel: "Amnezia",
      links: [
        { label: "Сайт", href: "https://amnezia.org/" },
        { label: "Скачать", href: "https://amnezia.org/downloads" },
        { label: "App Store", href: "https://apps.apple.com/us/app/amneziavpn/id1600529900" },
        { label: "Google Play", href: "https://play.google.com/store/apps/details?id=org.amnezia.vpn" },
        { label: "Windows", href: "https://amnezia.org/downloads" },
      ],
    });
  }
  if (hasHP) {
    options.push({
      key: "hp",
      protocol: "hysteria2",
      protocolLabel: "Hysteria2",
      appLabel: "Happ",
      links: [
        { label: "Сайт", href: "https://happ.su/" },
        { label: "App Store", href: "https://apps.apple.com/us/app/happ-proxy-utility/id6504287215" },
        { label: "Google Play", href: "https://play.google.com/store/apps/details?id=com.happproxy" },
        { label: "Windows", href: "https://github.com/Happ-proxy/happ-desktop/releases" },
      ],
    });
  }
  return options;
}

function protocolChoices(options) {
  const byProtocol = new Map();
  options.forEach((option) => {
    if (!byProtocol.has(option.protocol)) {
      byProtocol.set(option.protocol, option);
    }
  });
  return [...byProtocol.values()].map((option) => {
    const active = options.some((candidate) => candidate.protocol === option.protocol && candidate.key === selectedApp);
    return renderAppChoice(option.protocolLabel, option.key, active);
  });
}

function renderAppChoice(label, app, active = selectedApp === app) {
  return `<button type="button" class="app-choice ${active ? "active" : ""}" data-select-app="${escapeAttr(app)}">${escapeHTML(label)}</button>`;
}

function renderAppHelp(option) {
  const links = option.links || [];
  const commands = option.commands || [];
  return `
    <details class="app-help">
      <summary>О приложении ${escapeHTML(option.appLabel)}</summary>
      <div class="app-help-body">
        ${links.length > 0 ? `
          <div class="connect-actions">
            ${links.map((link) => `<a class="button" href="${escapeAttr(link.href)}" target="_blank" rel="noopener noreferrer">${escapeHTML(link.label)}</a>`).join("")}
          </div>
        ` : `<span class="subline">Ссылки на установку пока не заданы.</span>`}
        ${commands.length > 0 ? `
          <div class="install-commands">
            ${commands.map((item) => `
              <div>
                <span class="context-label">${escapeHTML(item.label)}</span>
                <code>${escapeHTML(item.command)}</code>
              </div>
            `).join("")}
          </div>
        ` : `<span class="subline">Команды для brew/apt пока не заданы.</span>`}
      </div>
    </details>
  `;
}

function policyText() {
  const policy = summary?.policy || {};
  const limit = Number(policy.connection_limit ?? 0);
  const used = availableConnections().length;
  if (limit === -1) {
    return `Можно создавать подключения без ограничения по количеству. Сейчас: ${used}.`;
  }
  if (limit > 0) {
    const left = Math.max(0, limit - used);
    if (left === 0) {
      return `Лимит подключений исчерпан: ${used} из ${limit}.`;
    }
    return `Можно создать еще ${left} ${plural(left, "подключение", "подключения", "подключений")}. Использовано: ${used} из ${limit}.`;
  }
  return "Самостоятельное создание подключений не разрешено.";
}

function canCreateConnection() {
  const policy = summary?.policy || {};
  const limit = Number(policy.connection_limit ?? 0);
  if (limit === -1) {
    return true;
  }
  return limit > availableConnections().length;
}

async function createConnections(connectionName, nodeIDs) {
  try {
    const response = await fetch("/client/v1/session/connections", {
      method: "POST",
      headers: strongHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ connection_name: connectionName, node_ids: nodeIDs }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось создать подключение");
      return false;
    }
    showToast(payload.partial ? "Часть подключений не создана" : "Подключение создано");
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function deleteConnection(connectionID) {
  try {
    const response = await fetch("/client/v1/session/connections", {
      method: "DELETE",
      headers: strongHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ connection_id: connectionID }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось удалить подключение");
      return false;
    }
    showToast("Подключение запланировано к удалению");
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

function openDeleteConnectionModal() {
  const connections = availableConnections();
  if (connections.length === 0) {
    showToast("Нет подключений для удаления");
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "delete-connection-confirm-dialog",
    title: "Удалить подключение?",
    initialFocus: "[name='connection_id']",
    body: `
      <form id="delete-connection-form" class="unlock-form">
        <label>
          Подключение
          <select name="connection_id" required>
            ${connections.map((item) => `
              <option value="${escapeAttr(item.connection.id)}" ${item.connection.id === selectedConnectionID ? "selected" : ""}>
                ${escapeHTML(item.connection.name || item.node?.name || "Подключение")}
              </option>
            `).join("")}
          </select>
        </label>
        <p class="subline">Конфигурация выбранного подключения перестанет работать после применения изменения.</p>
      </form>
    `,
    actions: `
      <button type="button" data-close-dialog>Отмена</button>
      <button type="submit" form="delete-connection-form" class="danger" id="confirm-delete-connection">Удалить</button>
    `,
  });
  dialog.querySelector("#delete-connection-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const connectionID = String(form.get("connection_id") || "");
    if (await deleteConnection(connectionID)) {
      dialog.close();
    }
  });
}

async function logoutSession() {
  try {
    await fetch("/client/v1/logout", {
      method: "POST",
      headers: strongHeaders(),
    });
  } finally {
    localStorage.removeItem(sessionKey);
    sessionToken = "";
    stopClientEvents();
    summary = null;
    unlockChallenge = "";
    selectedApp = "";
    selectedConnectionID = "";
    selectedHPProfile = "";
    renderUnlock();
  }
}

function confirmLogout() {
  const dialog = window.lazUI.openModal({
    id: "logout-confirm-dialog",
    title: "Выйти?",
    compact: true,
    initialFocus: "#confirm-logout",
    actions: `
      <button type="button" data-close-dialog>Отмена</button>
      <button type="button" class="primary" id="confirm-logout">Выйти</button>
    `,
  });
  dialog.querySelector("#confirm-logout").addEventListener("click", async () => {
    dialog.close();
    await logoutSession();
  });
}

async function rotateRecoveryCode() {
  try {
    const response = await fetch("/client/v1/session/recovery-code", {
      method: "POST",
      headers: strongHeaders(),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось обновить recovery code");
      return;
    }
    recoveryCode = payload.recovery_code || "";
    if (payload.sessions_revoked) {
      localStorage.removeItem(sessionKey);
      sessionToken = "";
      summary = null;
      unlockChallenge = "";
    }
    showToast("Recovery code обновлен");
    renderRecoveryResult();
  } catch (error) {
    showToast(error.message || String(error));
  }
}

function confirmRotateRecoveryCode() {
  const dialog = window.lazUI.openModal({
    id: "recovery-reset-confirm-dialog",
    title: "Сбросить recovery code?",
    compact: true,
    initialFocus: "#confirm-recovery-reset",
    body: `
      <div class="subline">
        <div>Будет создан новый recovery code.</div>
        <div>Старый recovery code перестанет работать.</div>
        <div>Пользователь будет разлогинен на всех устройствах.</div>
        <div>VPN-подключения не изменятся.</div>
      </div>
    `,
    actions: `
      <button type="button" data-close-dialog>Отмена</button>
      <button type="button" class="primary" id="confirm-recovery-reset">Сбросить</button>
    `,
  });
  dialog.querySelector("#confirm-recovery-reset").addEventListener("click", async () => {
    dialog.close();
    await rotateRecoveryCode();
  });
}

function sessionHeaders(extra = {}) {
  return {
    ...extra,
    Authorization: `Bearer ${sessionToken}`,
  };
}

function strongHeaders(extra = {}) {
  return strongCSRFToken
    ? { ...extra, "X-CSRF-Token": strongCSRFToken }
    : { ...extra };
}

function liteHeaders(extra = {}) {
  return liteCSRFToken
    ? { ...extra, "X-CSRF-Token": liteCSRFToken }
    : { ...extra };
}

async function solvePoW(signature, difficulty) {
  const encoder = new TextEncoder();
  for (let nonce = 0; ; nonce += 1) {
    const candidate = String(nonce);
    const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", encoder.encode(`${signature}:${candidate}`)));
    if (hashHasLeadingZeroBits(hash, difficulty)) {
      return candidate;
    }
    if (nonce % 1000 === 0) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
  }
}

function hashHasLeadingZeroBits(hash, bits) {
  const fullBytes = Math.floor(bits / 8);
  const remaining = bits % 8;
  for (let i = 0; i < fullBytes; i += 1) {
    if (hash[i] !== 0) {
      return false;
    }
  }
  if (remaining === 0) {
    return true;
  }
  const mask = 0xff << (8 - remaining);
  return (hash[fullBytes] & mask) === 0;
}

function renderAmneziaConfigs(configs) {
  if (configs.length === 0) {
    return "";
  }
  return `
    <section>
      <h2 class="section-title">Конфигурации</h2>
      <div class="connect-grid">
        ${configs.map(renderConfig).join("")}
      </div>
    </section>
  `;
}

function renderHPConfigs(enabled) {
  if (!enabled) {
    return "";
  }
  const profiles = hpProfiles();
  if (profiles.length === 0) {
    return "";
  }
  return `
    <section>
      <h2 class="section-title">Подписки</h2>
      <div class="subscription-list">
        ${profiles.map(renderHPProfileCard).join("")}
      </div>
    </section>
  `;
}

function hpProfiles() {
  return (summary?.profiles || [])
    .filter((profile) => profile.status === "active" && profile.client === "happ" && profile.kind === "hp_subscription")
    .map((profile) => {
      return {
        slug: profile.slug,
        name: profile.name || profile.slug,
        description: profile.description || "",
      };
    })
    .sort((left, right) => {
      if (left.slug === "all") return -1;
      if (right.slug === "all") return 1;
      return String(left.name).localeCompare(String(right.name));
    });
}

function renderHPProfileCard(profile) {
  const active = selectedHPProfile === profile.slug;
  return `
    <article class="subscription-card ${active ? "active" : ""}" data-client-sub-card="${escapeAttr(profile.slug)}">
      <div class="subscription-summary">
        <div class="subscription-copy">
          <h2>${escapeHTML(profile.name)}</h2>
          ${profile.description ? `<p class="subline">${escapeHTML(profile.description)}</p>` : ""}
        </div>
        <button type="button" data-view-client-sub="${escapeAttr(profile.slug)}">${active ? "Скрыть" : "Просмотреть"}</button>
      </div>
      ${active ? `
        <div class="subscription-details">
          <div class="connect-actions">
            <a class="button primary disabled" data-open-client-sub aria-disabled="true">Открыть</a>
            <button type="button" data-copy-client-sub data-value="" disabled>Скопировать</button>
          </div>
          <div class="qr-box" data-qr-client-sub><span class="muted">QR...</span></div>
        </div>
      ` : ""}
    </article>
  `;
}

async function loadSelectedConnectionSubscription() {
  if (selectedApp !== "hp" || !selectedHPProfile) {
    return;
  }
  const card = app.querySelector(`[data-client-sub-card="${cssEscape(selectedHPProfile)}"]`);
  if (card) {
    await loadClientSubscription(card);
  }
}

async function loadClientSubscription(card) {
  const target = card.querySelector("[data-qr-client-sub]");
  const open = card.querySelector("[data-open-client-sub]");
  const copy = card.querySelector("[data-copy-client-sub]");
  const profile = card.dataset.clientSubCard || "all";
  if (!target || !open || !copy) {
    return;
  }
  target.innerHTML = `<span class="muted">QR...</span>`;
  try {
    const path = sessionToken ? "/client/v1/session/hp-link" : "/client/v1/hp-link";
    const response = await fetch(path, {
      method: "POST",
      headers: sessionToken ? strongHeaders({ "Content-Type": "application/json" }) : liteHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({
        token: data.token || "",
        challenge: unlockChallenge,
        connection_id: selectedConnectionID,
        profile,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.url) {
      target.innerHTML = `<span class="muted">${escapeHTML(payload.error || "Не удалось создать ссылку")}</span>`;
      return;
    }
    open.href = payload.url;
    open.classList.remove("disabled");
    open.removeAttribute("aria-disabled");
    copy.dataset.value = payload.url;
    copy.disabled = false;
    await renderQRImage(target, sessionToken ? "/client/v1/session/qr" : "/client/v1/qr", {
      token: data.token || "",
      kind: "hp_link",
      id: payload.id || "",
    });
  } catch (error) {
    target.innerHTML = `<span class="muted">${escapeHTML(error.message || String(error))}</span>`;
  }
}

function renderConfig(config) {
  const openURL = openURLForConfig(config);
  const qrKey = `config_${config.id}`;
  const showQR = Boolean(openURL) || isAmneziaConfig(config);
  if (showQR) {
    qrReferences.set(qrKey, { kind: "config", id: config.id || "" });
  }
  return `
    <article class="connect-card">
      <div>
        <h2>${escapeHTML(config.name || config.slug || "Config")}</h2>
        <p class="subline">${config.import_url ? "AmneziaVPN · AmneziaWG" : escapeHTML(clientLabel(config))}</p>
      </div>
      <div class="connect-actions">
        ${openURL ? `<a class="button primary" href="${escapeAttr(openURL)}">${escapeHTML(openLabel(config))}</a>` : ""}
        <button type="button" data-copy="${escapeAttr(config.id)}">${config.import_url ? "Скопировать vpn://" : "Скопировать"}</button>
        ${config.kind === "amnezia_conf" ? `<button type="button" data-download-config="${escapeAttr(config.id)}">Скачать .conf</button>` : ""}
      </div>
      ${showQR ? `<div class="qr-box" data-qr="${escapeAttr(qrKey)}"><span class="muted">QR...</span></div>` : ""}
      ${config.import_url ? "" : `<pre>${escapeHTML(shortConfig(config.config))}</pre>`}
    </article>
  `;
}

function openURLForConfig(config) {
  const value = String(config.import_url || config.config || "").trim();
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    return value;
  }
  return "";
}

function openLabel(config) {
  if (isAmneziaConfig(config)) {
    return "Открыть в AmneziaVPN";
  }
  if (config.client === "happ" || config.kind === "hy2_uri") {
    return "Открыть в Happ";
  }
  return "Открыть";
}

function clientLabel(config) {
  const parts = [];
  if (config.client) {
    parts.push(config.client);
  }
  if (config.kind) {
    parts.push(config.kind);
  }
  if (config.slug) {
    parts.push(config.slug);
  }
  return parts.join(" · ");
}

async function renderQRCodes() {
  for (const [key, reference] of qrReferences.entries()) {
    const target = app.querySelector(`[data-qr="${cssEscape(key)}"]`);
    if (!target) {
      continue;
    }
    await renderQRImage(target, sessionToken ? "/client/v1/session/qr" : "/client/v1/qr", {
      token: data.token || "",
      ...reference,
    });
  }
}

async function renderQRImage(target, path, body) {
  try {
    const response = await fetch(path, {
      method: "POST",
      headers: sessionToken ? strongHeaders({ "Content-Type": "application/json" }) : liteHeaders({ "Content-Type": "application/json" }),
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

function isAmneziaConfig(config) {
  return config.client === "amnezia" || String(config.kind || "").startsWith("amnezia");
}

function availableConnections() {
  return (summary?.connections || [])
    .filter((item) => item?.connection && item.connection.desired_status !== "deleted")
    .sort((a, b) => (a.connection.name || a.connection.id).localeCompare(b.connection.name || b.connection.id));
}

function configsForConnection(connectionID) {
  if (!connectionID) {
    return [];
  }
  return (summary?.configs || []).filter((config) => config.connection_id === connectionID);
}

async function copyText(value) {
  if (!value) {
    showToast("Нечего копировать");
    return;
  }
  await navigator.clipboard.writeText(value);
  showToast("Скопировано");
}

function plural(count, one, few, many) {
  const n = Math.abs(Number(count));
  if (n % 10 === 1 && n % 100 !== 11) {
    return one;
  }
  if (n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20)) {
    return few;
  }
  return many;
}

function setPageHeader(value = null) {
  if (!pageTitle) {
    return;
  }
  pageTitle.textContent = summaryTitle(value) || "Пользователь";
}

function summaryTitle(value) {
  if (!value?.account) {
    return "";
  }
  const account = value.account;
  const name = account.display_name || account.username;
  return `Пользователь ${name}`;
}

function shortConfig(value) {
  value = String(value || "").trim();
  if (value.length <= 240) {
    return value;
  }
  return `${value.slice(0, 240)}...`;
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
