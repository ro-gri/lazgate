const data = JSON.parse(document.querySelector("#connect-data").textContent || "{}");
let summary = null;
let selectedApp = "";
let selectedClientID = "";
let selectedHPProfile = "";
const app = document.querySelector("#connect-app");
const pageTitle = document.querySelector("#connect-title");
const userLine = document.querySelector("#connect-account");
const toast = document.querySelector("#toast");
const qrValues = new Map();
let unlockChallenge = "";
const sessionKey = "laz_client_session";
let sessionToken = localStorage.getItem(sessionKey) || "";
let recoveryCode = "";
let eventSource = null;
let eventReloadTimer = 0;

setPageHeader();
userLine.innerHTML = sessionToken ? `<span class="subline">Войти по сохраненной сессии</span>` : `<span class="subline">Введите проверочный код</span>`;
if (sessionToken) {
  loadSession();
} else {
  renderUnlock();
}

function renderUnlock() {
  setPageHeader();
  userLine.innerHTML = `<span class="subline">Введите проверочный код</span>`;
  app.innerHTML = `
    <article class="connect-card">
      <div>
        <h2>Доступ</h2>
        <p class="subline">Введите первые две и последние две буквы имени аккаунта.</p>
      </div>
      ${renderLinkUnlockForm()}
    </article>
  `;
  const unlockForm = app.querySelector("#unlock-form");
  if (unlockForm) {
    unlockForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const form = new FormData(event.currentTarget);
      await unlock(String(form.get("access_check") || ""));
    });
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
        <input name="pin" type="password" required autocomplete="current-password">
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
        <input name="new_pin" type="password" required autocomplete="new-password">
      </label>
      <div class="connect-actions">
        <button type="submit" class="primary">Сбросить пароль</button>
        ${secondaryAction}
      </div>
    </form>
  `;
}

async function unlock(challenge) {
  try {
    const response = await fetch("/client/v1/configs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: data.token || "",
        challenge,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось открыть");
      return;
    }
    unlockChallenge = challenge;
    summary = payload || {};
    setPageHeader(summary);
    userLine.innerHTML = "";
    startClientEvents();
    render();
  } catch (error) {
    showToast(error.message || String(error));
  }
}

async function setupPIN(pin) {
  try {
    const response = await fetch("/client/v1/setup-pin", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: data.token || "",
        challenge: unlockChallenge,
        pin,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось задать пароль");
      return false;
    }
    recoveryCode = payload.recovery_code || "";
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

async function login(pin) {
  try {
    const response = await fetch("/client/v1/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: data.token || "",
        challenge: unlockChallenge,
        pin,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось войти");
      return false;
    }
    sessionToken = payload.session_token || "";
    localStorage.setItem(sessionKey, sessionToken);
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function recoverPIN(recovery_code, new_pin) {
  try {
    const response = await fetch("/client/v1/recover", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: data.token || "",
        challenge: unlockChallenge,
        recovery_code,
        new_pin,
      }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось восстановить");
      return false;
    }
    recoveryCode = payload.recovery_code || "";
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
      headers: sessionHeaders(),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      localStorage.removeItem(sessionKey);
      sessionToken = "";
      stopClientEvents();
      showToast(payload.error || "Сессия недействительна");
      renderUnlock();
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
  const params = new URLSearchParams();
  if (sessionToken) {
    params.set("session_token", sessionToken);
  } else if (data.token && unlockChallenge) {
    params.set("token", data.token || "");
    params.set("challenge", unlockChallenge);
  } else {
    return;
  }
  const source = new EventSource(`/client/v1/events?${params.toString()}`);
  eventSource = source;
  source.onmessage = handleClientEvent;
  ["client.created", "client.deleted", "connection.created", "connection.deleted"].forEach((type) => {
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

function handleClientEvent(event) {
  let payload = {};
  try {
    payload = JSON.parse(event.data || "{}");
  } catch (_) {
    return;
  }
  if (payload.message) {
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
  const clients = availableClients();
  if (!selectedClientID || !clients.some((client) => client.id === selectedClientID)) {
    selectedClientID = clients[0]?.id || "";
  }
  const configs = configsForClient(selectedClientID);
  const amneziaConfigs = configs.filter(isAmneziaConfig);
  const hasHysteria = connectionsForClient(selectedClientID).some((item) => item.connection?.protocol === "hysteria2");
  if (!selectedApp || (selectedApp === "amnezia" && amneziaConfigs.length === 0) || (selectedApp === "hp" && !hasHysteria)) {
    selectedApp = amneziaConfigs.length > 0 ? "amnezia" : "hp";
  }
  const profiles = hpProfiles();
  if (selectedHPProfile && !profiles.some((profile) => profile.slug === selectedHPProfile)) {
    selectedHPProfile = "";
  }
  renderClientHeaderActions();
  qrValues.clear();
  app.innerHTML = `
    ${renderSessionTools()}
    ${clients.length === 0 ? `<section class="empty">Нет активных клиентов с конфигурациями.</section>` : ""}
    ${renderConnectionContext(clients, amneziaConfigs.length > 0, hasHysteria)}
    ${clients.length > 0 && amneziaConfigs.length === 0 && !hasHysteria ? `<section class="empty">Нет активных конфигураций для выбранного клиента.</section>` : ""}
    ${selectedApp === "amnezia" ? renderAmneziaConfigs(amneziaConfigs) : ""}
    ${selectedApp === "hp" ? renderHPConfigs(hasHysteria) : ""}
  `;
  window.lazUI.bindDialogClose(app);
  app.querySelectorAll("[data-select-client]").forEach((button) => {
    button.addEventListener("click", () => {
      selectedClientID = button.dataset.selectClient;
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
      copyText(cfg?.config || "");
    });
  });
  app.querySelectorAll("[data-copy-client-sub]").forEach((button) => {
    button.addEventListener("click", () => copyText(button.dataset.value || ""));
  });
  const openPinDialog = app.querySelector("#open-pin-dialog");
  if (openPinDialog) {
    openPinDialog.addEventListener("click", openPinModal);
  }
  const logout = userLine.querySelector("#client-logout");
  if (logout) {
    logout.addEventListener("click", confirmLogout);
  }
  const rotateRecovery = userLine.querySelector("#rotate-recovery-code");
  if (rotateRecovery) {
    rotateRecovery.addEventListener("click", confirmRotateRecoveryCode);
  }
  const deleteClientButton = app.querySelector("#open-client-delete");
  if (deleteClientButton) {
    deleteClientButton.addEventListener("click", openDeleteClientModal);
  }
  const copyRecovery = app.querySelector("#copy-recovery-code");
  if (copyRecovery) {
    copyRecovery.addEventListener("click", () => copyText(recoveryCode));
  }
  const openFullAccess = app.querySelector("#open-full-access");
  if (openFullAccess) {
    openFullAccess.addEventListener("click", openFullAccessModal);
  }
  const openClient = app.querySelector("#open-client-create");
  if (openClient) {
    openClient.addEventListener("click", openCreateClientModal);
  }
  renderQRCodes();
  loadSelectedClientSubscription();
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
        <span>Открыт базовый доступ. Для создания клиентов нужен пароль.</span>
        <button type="button" id="open-pin-dialog">Задать пароль</button>
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
          ${canCreateClient() ? `<button type="button" id="open-client-create">Создать клиент</button>` : ""}
          ${availableClients().length > 0 ? `<button type="button" id="open-client-delete">Удалить клиент</button>` : ""}
        </div>
      </section>
    `);
  }
  return blocks.join("");
}

function openCreateClientModal() {
  const next = availableClients().length + 1;
  const dialog = window.lazUI.openModal({
    id: "client-create-dialog",
    title: "Создать клиент",
    initialFocus: "[name='client_name']",
    body: `
      <form id="client-form" class="unlock-form">
        <label>
          Client
          <input name="client_name" required autocomplete="off" value="${escapeAttr(`Client ${next}`)}">
        </label>
        <label>
          Client slug
          <input name="client_slug" required autocomplete="off" autocapitalize="none" spellcheck="false" value="${escapeAttr(window.lazUI.slugifyInput(`Client ${next}`))}">
        </label>
        <div class="connect-actions">
          <button type="submit" class="primary">Создать клиент</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#client-form");
  wireClientSlugAutofill(form);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    if (await createClient(String(data.get("client_slug") || ""), String(data.get("client_name") || ""))) {
      dialog.close();
    }
  });
}

function wireClientSlugAutofill(form) {
  const name = form.elements.client_name;
  const slug = form.elements.client_slug;
  let edited = false;
  slug.addEventListener("input", () => {
    edited = true;
  });
  name.addEventListener("input", () => {
    if (!edited) {
      slug.value = window.lazUI.slugifyInput(name.value);
    }
  });
}

function openPinModal() {
  const dialog = window.lazUI.openModal({
    id: "pin-dialog",
    title: "Пароль",
    initialFocus: "[name='pin']",
    body: `
        <p class="subline">Пароль включает полный доступ: создание клиентов и управление recovery code после повторного входа.</p>
        ${recoveryCode ? `<pre>${escapeHTML(recoveryCode)}</pre>` : ""}
        <form id="pin-form" class="unlock-form">
          <label>
            Новый пароль
            <input name="pin" type="password" required autocomplete="new-password" minlength="6">
          </label>
          <div class="connect-actions">
            <button type="submit" class="primary">Задать пароль</button>
            ${recoveryCode ? `<button type="button" id="copy-recovery-code">Скопировать recovery code</button>` : ""}
          </div>
        </form>
    `,
  });
  const form = dialog.querySelector("#pin-form");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    if (await setupPIN(String(data.get("pin") || ""))) {
      dialog.close();
    }
  });
}

function openFullAccessModal() {
  const dialog = window.lazUI.openModal({
    id: "full-access-dialog",
    title: "Полный доступ",
    initialFocus: "[name='pin']",
    body: renderLoginForm("full-access-login-form", `<button type="button" id="show-recovery-form">Recovery</button>`),
  });
  dialog.querySelector("#full-access-login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    if (await login(String(form.get("pin") || ""))) {
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
    if (await recoverPIN(String(form.get("recovery_code") || ""), String(form.get("new_pin") || ""))) {
      dialog.close();
    }
  });
  dialog.querySelector("#show-login-form").addEventListener("click", () => {
    dialog.close();
    openFullAccessModal();
  });
}

function renderConnectionContext(clients, hasAmnezia, hasHP) {
  if (clients.length === 0) {
    return "";
  }
  const options = availableAppOptions(hasAmnezia, hasHP);
  const protocolItems = protocolChoices(options);
  const appItems = options.map((option) => renderAppChoice(option.appLabel, option.key));
  const selectedOption = options.find((option) => option.key === selectedApp) || options[0];
  return `
    <section class="selection-panel">
      <div class="selection-row">
        <span class="context-label">Клиент</span>
        <div class="connect-actions">
          ${clients.map((client) => `
            <button type="button" class="app-choice ${selectedClientID === client.id ? "active" : ""}" data-select-client="${escapeAttr(client.id)}">
              ${escapeHTML(client.name || client.slug || "Client")}
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
  const limit = Number(policy.client_limit ?? 0);
  const used = availableClients().length;
  if (limit === -1) {
    return `Можно создавать клиентов без ограничения по количеству. Сейчас: ${used}.`;
  }
  if (limit > 0) {
    const left = Math.max(0, limit - used);
    if (left === 0) {
      return `Лимит клиентов исчерпан: ${used} из ${limit}.`;
    }
    return `Можно создать еще ${left} ${plural(left, "клиент", "клиента", "клиентов")}. Использовано: ${used} из ${limit}.`;
  }
  return "Самостоятельное создание клиентов не разрешено.";
}

function canCreateClient() {
  const policy = summary?.policy || {};
  const limit = Number(policy.client_limit ?? 0);
  if (limit === -1) {
    return true;
  }
  return limit > availableClients().length;
}

async function createClient(client_slug, client_name) {
  try {
    const response = await fetch("/client/v1/session/clients", {
      method: "POST",
      headers: sessionHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ client_slug, client_name }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось создать клиент");
      return false;
    }
    showToast(payload.partial ? "Клиент создан частично" : "Клиент создан");
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

async function deleteClient(clientID) {
  try {
    const response = await fetch("/client/v1/session/clients", {
      method: "DELETE",
      headers: sessionHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ client_id: clientID }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      showToast(payload.error || "Не удалось удалить клиент");
      return false;
    }
    showToast(payload.partial ? "Клиент удален частично" : "Клиент удален");
    await loadSession();
    return true;
  } catch (error) {
    showToast(error.message || String(error));
    return false;
  }
}

function openDeleteClientModal() {
  const clients = availableClients();
  if (clients.length === 0) {
    showToast("Нет клиентов для удаления");
    return;
  }
  const dialog = window.lazUI.openModal({
    id: "delete-client-confirm-dialog",
    title: "Удалить клиент?",
    initialFocus: "[name='client_id']",
    body: `
      <form id="delete-client-form" class="unlock-form">
        <label>
          Клиент
          <select name="client_id" required>
            ${clients.map((client) => `
              <option value="${escapeAttr(client.id)}" ${client.id === selectedClientID ? "selected" : ""}>
                ${escapeHTML(client.name || client.slug || "Client")}
              </option>
            `).join("")}
          </select>
        </label>
        <p class="subline">VPN-подключения выбранного клиента будут удалены.</p>
      </form>
    `,
    actions: `
      <button type="button" data-close-dialog>Отмена</button>
      <button type="submit" form="delete-client-form" class="danger" id="confirm-delete-client">Удалить</button>
    `,
  });
  dialog.querySelector("#delete-client-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const clientID = String(form.get("client_id") || "");
    if (await deleteClient(clientID)) {
      dialog.close();
    }
  });
}

async function logoutSession() {
  try {
    await fetch("/client/v1/logout", {
      method: "POST",
      headers: sessionHeaders(),
    });
  } finally {
    localStorage.removeItem(sessionKey);
    sessionToken = "";
    stopClientEvents();
    summary = null;
    unlockChallenge = "";
    selectedApp = "";
    selectedClientID = "";
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
      headers: sessionHeaders(),
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

async function loadSelectedClientSubscription() {
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
      headers: sessionToken ? sessionHeaders({ "Content-Type": "application/json" }) : { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: data.token || "",
        challenge: unlockChallenge,
        client_id: selectedClientID,
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
      value: payload.url,
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
    qrValues.set(qrKey, config.config || "");
  }
  return `
    <article class="connect-card">
      <div>
        <h2>${escapeHTML(config.name || config.slug || "Config")}</h2>
        <p class="subline">${escapeHTML(clientLabel(config))}</p>
      </div>
      <div class="connect-actions">
        ${openURL ? `<a class="button primary" href="${escapeAttr(openURL)}">${escapeHTML(openLabel(config))}</a>` : ""}
        <button type="button" data-copy="${escapeAttr(config.id)}">Скопировать</button>
      </div>
      ${showQR ? `<div class="qr-box" data-qr="${escapeAttr(qrKey)}"><span class="muted">QR...</span></div>` : ""}
      <pre>${escapeHTML(shortConfig(config.config))}</pre>
    </article>
  `;
}

function openURLForConfig(config) {
  const value = String(config.config || "").trim();
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    return value;
  }
  return "";
}

function openLabel(config) {
  if (config.kind === "amnezia_vpn" || config.client === "amnezia") {
    return "Открыть в Amnezia";
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
  for (const [key, value] of qrValues.entries()) {
    const target = app.querySelector(`[data-qr="${cssEscape(key)}"]`);
    if (!target) {
      continue;
    }
    await renderQRImage(target, sessionToken ? "/client/v1/session/qr" : "/client/v1/qr", {
      token: data.token || "",
      value,
    });
  }
}

async function renderQRImage(target, path, body) {
  try {
    const response = await fetch(path, {
      method: "POST",
      headers: sessionToken ? sessionHeaders({ "Content-Type": "application/json" }) : { "Content-Type": "application/json" },
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

function availableClients() {
  if (Array.isArray(summary?.clients) && summary.clients.length > 0) {
    return summary.clients
      .filter((client) => client?.status === "active")
      .map((client) => ({
        id: client.id,
        slug: client.slug || "",
        name: client.name || "",
      }))
      .sort((a, b) => (a.name || a.slug || a.id).localeCompare(b.name || b.slug || b.id));
  }
  const byID = new Map();
  for (const item of summary?.connections || []) {
    if (!item?.connection?.client_id) {
      continue;
    }
    const client = item.client || {};
    byID.set(item.connection.client_id, {
      id: item.connection.client_id,
      slug: client.slug || "",
      name: client.name || "",
    });
  }
  if (byID.size === 0 && summary?.client_id) {
    const client = summary.client || {};
    byID.set(summary.client_id, {
      id: summary.client_id,
      slug: client.slug || "",
      name: client.name || "",
    });
  }
  return [...byID.values()].sort((a, b) => (a.name || a.slug || a.id).localeCompare(b.name || b.slug || b.id));
}

function configsForClient(clientID) {
  if (!clientID) {
    return [];
  }
  const accessIDs = new Set();
  for (const item of summary?.connections || []) {
    if (item?.connection?.client_id === clientID) {
      accessIDs.add(item.connection.id);
    }
  }
  return (summary?.configs || []).filter((config) => accessIDs.has(config.connection_id));
}

function connectionsForClient(clientID) {
  if (!clientID) {
    return [];
  }
  return (summary?.connections || []).filter((item) => item?.connection?.client_id === clientID);
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
