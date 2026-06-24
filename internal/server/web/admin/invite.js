const webPrefix = document.body.dataset.webPrefix || "/admin";
const toast = document.querySelector("#toast");
const form = document.querySelector("#invite-form");
const token = decodeURIComponent(location.pathname.split("/").filter(Boolean).pop() || "");

function showToast(message) {
  window.lazUI.showToast(toast, message);
}

async function enroll(body) {
  const response = await fetch(`${webPrefix}/api/v1/auth/enroll-admin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || payload.message || "Не удалось зарегистрироваться");
  }
  return payload;
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(form);
  const password = String(data.get("password") || "");
  const confirmPassword = String(data.get("confirm_password") || "");
  if (password !== confirmPassword) {
    showToast("Пароли не совпадают");
    return;
  }
  if (!/(?=.*[A-Za-z])(?=.*\d)(?=.*[^A-Za-z0-9]).{6,}/.test(password)) {
    showToast("Пароль: минимум 6 символов, буква, цифра и символ");
    return;
  }
  try {
    const result = await enroll({
      invite_token: token,
      invite_code: String(data.get("invite_code") || "").trim(),
      login: String(data.get("login") || "").trim(),
      display_name: String(data.get("display_name") || "").trim(),
      password,
    });
    localStorage.setItem(`laz_admin_csrf:${webPrefix}`, result.csrf_token || "");
    localStorage.setItem(`laz_admin_refresh_after:${webPrefix}`, result.refresh_after || "");
    location.href = `${webPrefix}/`;
  } catch (error) {
    showToast(error.message || "Не удалось зарегистрироваться");
  }
});
