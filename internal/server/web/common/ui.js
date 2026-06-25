(function () {
  const closeBound = new WeakSet();
  let pendingRequests = 0;
  let busyTimer = 0;
  let busyEl = null;

  function escapeAttr(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  function modalHTML(options = {}) {
    const id = options.id ? ` id="${escapeAttr(options.id)}"` : "";
    const titleID = options.titleID ? ` id="${escapeAttr(options.titleID)}"` : "";
    const title = options.title || "";
    const closeLabel = options.closeLabel || "Закрыть";
    const className = [
      "laz-modal",
      options.wide ? "wide-dialog" : "",
      options.compact ? "compact-dialog" : "",
      options.className || "",
    ].filter(Boolean).join(" ");
    const actions = options.actions ? `<div class="laz-modal-actions">${options.actions}</div>` : "";
    return `
      <dialog${id} class="${escapeAttr(className)}">
        <div class="laz-modal-body">
          <div class="laz-modal-head">
            <h2${titleID}>${escapeAttr(title)}</h2>
            <button type="button" class="icon-button" data-close-dialog aria-label="${escapeAttr(closeLabel)}">×</button>
          </div>
          ${options.body || ""}
          ${actions}
        </div>
      </dialog>
    `;
  }

  function bindDialogClose(root = document) {
    root.querySelectorAll("[data-close-dialog]").forEach((button) => {
      if (closeBound.has(button)) {
        return;
      }
      closeBound.add(button);
      button.addEventListener("click", () => {
        const dialog = button.closest("dialog");
        if (dialog) {
          dialog.close();
        }
      });
    });
  }

  function openModal(options = {}) {
    const holder = document.createElement("div");
    holder.innerHTML = modalHTML(options).trim();
    const dialog = holder.firstElementChild;
    document.body.appendChild(dialog);
    bindDialogClose(dialog);
    dialog.addEventListener("close", () => dialog.remove(), { once: true });
    dialog.showModal();
    const focusTarget = options.initialFocus ? dialog.querySelector(options.initialFocus) : null;
    if (focusTarget && typeof focusTarget.focus === "function") {
      focusTarget.focus();
      if (typeof focusTarget.select === "function") {
        focusTarget.select();
      }
    }
    return dialog;
  }

  function showToast(toast, message, options = {}) {
    if (!toast) {
      return;
    }
    const openDialog = document.querySelector("dialog[open]");
    if (openDialog) {
      openDialog.appendChild(toast);
      toast.classList.add("in-dialog");
    } else {
      document.body.appendChild(toast);
      toast.classList.remove("in-dialog");
    }
    toast.textContent = message;
    toast.classList.add("visible");
    clearTimeout(toast._lazTimer);
    toast._lazTimer = setTimeout(() => toast.classList.remove("visible"), options.timeout || 2200);
  }

  function installBusyIndicator() {
    if (typeof window.fetch !== "function" || window.fetch._lazBusyWrapped) {
      return;
    }
    const nativeFetch = window.fetch.bind(window);
    const wrappedFetch = async (...args) => {
      beginBusy();
      try {
        return await nativeFetch(...args);
      } finally {
        endBusy();
      }
    };
    wrappedFetch._lazBusyWrapped = true;
    window.fetch = wrappedFetch;
  }

  function beginBusy() {
    pendingRequests += 1;
    if (pendingRequests === 1) {
      clearTimeout(busyTimer);
      busyTimer = setTimeout(showBusy, 180);
    }
  }

  function endBusy() {
    pendingRequests = Math.max(0, pendingRequests - 1);
    if (pendingRequests === 0) {
      clearTimeout(busyTimer);
      hideBusy();
    }
  }

  function showBusy() {
    if (pendingRequests === 0) {
      return;
    }
    if (!busyEl) {
      busyEl = document.createElement("div");
      busyEl.className = "busy-indicator";
      busyEl.setAttribute("role", "status");
      busyEl.setAttribute("aria-live", "polite");
      busyEl.innerHTML = `<span class="busy-spinner" aria-hidden="true"></span><span>Запрос выполняется</span>`;
      document.body.appendChild(busyEl);
    }
    placeBusyIndicator();
    busyEl.classList.add("visible");
  }

  function hideBusy() {
    if (busyEl) {
      busyEl.classList.remove("visible");
      busyEl.classList.remove("in-dialog");
      document.body.appendChild(busyEl);
    }
  }

  function placeBusyIndicator() {
    const openDialog = document.querySelector("dialog[open]");
    if (openDialog) {
      const body = openDialog.querySelector(".laz-modal-body") || openDialog;
      body.appendChild(busyEl);
      busyEl.classList.add("in-dialog");
      return;
    }
    document.body.appendChild(busyEl);
    busyEl.classList.remove("in-dialog");
  }

  function slugifyInput(value) {
    const raw = transliterate(String(value || "").trim().toLowerCase());
    let out = "";
    let prevDash = false;
    for (const char of raw.normalize("NFKD")) {
      const code = char.charCodeAt(0);
      const ok = (code >= 97 && code <= 122) || (code >= 48 && code <= 57);
      if (ok) {
        out += char;
        prevDash = false;
        continue;
      }
      if (!prevDash) {
        out += "-";
        prevDash = true;
      }
    }
    out = out.replace(/^-+|-+$/g, "");
    return out || "default";
  }

  function transliterate(value) {
    const map = {
      а: "a", б: "b", в: "v", г: "g", д: "d", е: "e", ё: "e", ж: "zh", з: "z",
      и: "i", й: "y", к: "k", л: "l", м: "m", н: "n", о: "o", п: "p", р: "r",
      с: "s", т: "t", у: "u", ф: "f", х: "h", ц: "ts", ч: "ch", ш: "sh",
      щ: "sch", ъ: "", ы: "y", ь: "", э: "e", ю: "yu", я: "ya",
    };
    return [...value].map((char) => map[char] ?? char).join("");
  }

  window.lazUI = {
    bindDialogClose,
    beginBusy,
    endBusy,
    modalHTML,
    openModal,
    slugifyInput,
    showToast,
  };

  installBusyIndicator();
})();
