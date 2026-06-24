const webPrefix = document.body.dataset.webPrefix || "/admin";
const csrfKey = `laz_admin_csrf:${webPrefix}`;
const form = document.querySelector("#login-form");
const input = document.querySelector("#admin-token");
const toast = document.querySelector("#toast");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const token = input.value.trim();
  if (!token) {
    return;
  }
  const response = await fetch("/api/v1/auth/login", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || !payload.csrf_token) {
    input.value = "";
    input.focus();
    window.lazUI.showToast(toast, payload.error || "Не удалось войти");
    return;
  }
  localStorage.setItem(csrfKey, payload.csrf_token);
  location.href = `${webPrefix}/`;
});
