const webPrefix = document.body.dataset.webPrefix || "/admin";
const apiBase = `${webPrefix}/api/v1`;
const csrfKey = `laz_admin_csrf:${webPrefix}`;
const refreshAfterKey = `laz_admin_refresh_after:${webPrefix}`;
const form = document.querySelector("#login-form");
const loginInput = document.querySelector("#admin-login");
const passwordInput = document.querySelector("#admin-password");
const legacyTokenInput = document.querySelector("#admin-token");
const toast = document.querySelector("#toast");

let pendingPasswordChangeToken = "";

function clearPasswordChangeState() {
  pendingPasswordChangeToken = "";
  localStorage.removeItem(csrfKey);
  if (passwordInput) {
    passwordInput.value = "";
    passwordInput.focus();
  }
  if (legacyTokenInput) {
    legacyTokenInput.value = "";
  }
}

function showModalError(dialog, message) {
  const error = dialog.querySelector("#password-change-error");
  if (error) {
    error.textContent = message;
    error.hidden = false;
  }
  window.lazUI.showToast(toast, message);
}

function storeAdminAuthTiming(payload) {
  localStorage.setItem(csrfKey, payload.csrf_token);
  if (payload.refresh_after) {
    localStorage.setItem(refreshAfterKey, payload.refresh_after);
  } else {
    localStorage.removeItem(refreshAfterKey);
  }
}

function openPasswordChangeModal(passwordChangeToken) {
  pendingPasswordChangeToken = passwordChangeToken;
  const dialog = window.lazUI.openModal({
    id: "password-change-dialog",
    title: "Смените пароль администратора",
    closeLabel: "Отменить смену пароля",
    initialFocus: "#new-admin-password",
    body: `
      <form id="password-change-form" method="dialog" class="laz-modal-fields">
        <section class="notice warn">Пароль нужно изменить перед входом в админ-панель.</section>
        <label>
          Новый пароль
          <input id="new-admin-password" name="new_password" type="password" required minlength="12" autocomplete="new-password">
          <span class="helper">Минимум 12 символов.</span>
        </label>
        <label>
          Повторите пароль
          <input name="confirm_password" type="password" required minlength="12" autocomplete="new-password">
        </label>
        <p id="password-change-error" class="notice warn" role="alert" hidden></p>
        <div class="laz-modal-actions">
          <button type="button" data-close-dialog>Отмена</button>
          <button type="submit" class="primary">Сменить пароль и войти</button>
        </div>
      </form>
    `,
  });
  const form = dialog.querySelector("#password-change-form");
  const submitButton = form.querySelector('button[type="submit"]');
  let passwordChanged = false;

  dialog.addEventListener("cancel", (event) => {
    event.preventDefault();
    dialog.close();
  });
  dialog.addEventListener("close", () => {
    if (!passwordChanged) {
      clearPasswordChangeState();
      window.lazUI.showToast(toast, "Пароль нужно изменить перед входом");
    }
  }, { once: true });

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const newPassword = String(data.get("new_password") || "");
    const confirmation = String(data.get("confirm_password") || "");
    if (newPassword.length < 12) {
      showModalError(dialog, "Пароль должен быть не короче 12 символов");
      return;
    }
    if (newPassword !== confirmation) {
      showModalError(dialog, "Пароли не совпадают");
      return;
    }
    submitButton.disabled = true;
    try {
      const changeResponse = await fetch(`${apiBase}/auth/change-password`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          password_change_token: pendingPasswordChangeToken,
          new_password: newPassword,
        }),
      });
      const changePayload = await changeResponse.json().catch(() => ({}));
      if (!changeResponse.ok || !changePayload.csrf_token) {
        showModalError(dialog, changePayload.error || "Не удалось изменить пароль");
        return;
      }
      passwordChanged = true;
      pendingPasswordChangeToken = "";
      storeAdminAuthTiming(changePayload);
      location.href = `${webPrefix}/`;
    } catch (error) {
      showModalError(dialog, error.message || "Не удалось изменить пароль");
    } finally {
      submitButton.disabled = false;
    }
  });
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const login = loginInput?.value.trim() || "";
  const password = passwordInput?.value || "";
  const legacyToken = legacyTokenInput?.value.trim() || "";
  if (!password && !legacyToken) {
    window.lazUI.showToast(toast, "Введите пароль администратора");
    passwordInput?.focus();
    return;
  }
  const body = password
    ? { login: login || "admin", password }
    : { token: legacyToken };
  const response = await fetch(`${apiBase}/auth/login`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => ({}));
  if (response.ok && payload.must_change_password && payload.password_change_token) {
    openPasswordChangeModal(payload.password_change_token);
    return;
  }
  if (!response.ok || !payload.csrf_token) {
    if (passwordInput) {
      passwordInput.value = "";
      passwordInput.focus();
    }
    if (legacyTokenInput) {
      legacyTokenInput.value = "";
    }
    window.lazUI.showToast(toast, payload.error || "Не удалось войти");
    return;
  }
  storeAdminAuthTiming(payload);
  location.href = `${webPrefix}/`;
});
