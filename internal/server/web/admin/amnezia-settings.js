(function () {
  const timingDefaults = {RekeyAfterTime:"120", RekeyTimeout:"5", RejectAfterTime:"180", KeepaliveTimeout:"10", MaxHandshakeAttempts:"18"};

  function escape(value) {
    return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
  }

  function field(name, label, value, attributes = "") {
    return `<label>${escape(label)}<input name="${name}" value="${escape(value)}" autocomplete="off" ${attributes}></label>`;
  }

  function parameterFields(config, prefix, count, attributes) {
    return Array.from({ length: count }, (_, i) => {
      const key = prefix + (i + 1);
      return field(key.toLowerCase(), key, prefix === "I" ? config[key.toLowerCase()] || "" : config[key.toLowerCase()], attributes);
    }).join("");
  }

  function section(title, body, open = false) {
    return `<details class="awg-settings-section"${open ? " open" : ""}><summary>${escape(title)}</summary><div class="awg-settings-content">${body}</div></details>`;
  }

  function renderFields(config) {
    const routes = String(config.client_route_allowed_ips || "").split(",").map((s) => s.trim()).filter(Boolean);
    const allIPv4 = config.client_route_mode ? config.client_route_mode === "all" : routes.includes("0.0.0.0/0");
    const excludeIPv4 = config.client_route_mode === "exclude";
    const prefix = String(config.client_ipv4_cidr || "").split("/")[1] || "24";
    const timers = Object.keys(timingDefaults);
    return `<div data-awg-settings class="laz-modal-fields">
      <a href="https://github.com/amnezia-vpn/amneziawg-go#configuration" target="_blank" rel="noopener noreferrer">Описание параметров протокола ↗</a>
      ${section("Основные настройки", `<div class="grid-two">
        ${field("endpoint_host", "Публичный адрес", config.endpoint_host, 'required spellcheck="false"')}
        ${field("endpoint_port", "Порт", config.endpoint_port, 'type="number" min="1" max="65535" required')}
        </div>${field("dns", "DNS клиентов", config.dns, 'spellcheck="false"')}`, true)}
      ${section("Основные настройки Amnezia", `
        <div class="awg-parameter-grid">
          ${field("jc", "Jc", config.jc, 'type="number" min="0" max="65535"')}
          ${field("jmin", "Jmin", config.jmin, 'type="number" min="0" max="65535"')}
          ${field("jmax", "Jmax", config.jmax, 'type="number" min="0" max="65535"')}
        </div>
        <div class="awg-parameter-grid">${parameterFields(config, "S", 4, 'type="number" min="0" max="65535"')}</div>
        <div class="awg-parameter-grid">${parameterFields(config, "H", 4, 'inputmode="numeric" placeholder="Число или диапазон" spellcheck="false"')}</div>
        <div class="awg-key-setting"><span>HeaderProtectionKey</span><div class="awg-key-actions"><span data-header-key-status role="status" class="status ${config.header_protection_key_set ? "active" : "held"}">${config.header_protection_key_set ? "Задан" : "Не задан"}</span><button type="button" data-header-key-generate>Перегенерировать</button></div></div>
        <input type="hidden" name="header_protection_key" value="" disabled>
        <p data-header-key-error role="alert" hidden></p>
        ${field("content_padding_addition", "ContentPaddingAddition", config.content_padding_addition, 'placeholder="Число или диапазон" spellcheck="false"')}
        <div class="grid-two">${parameterFields(config, "I", 5, 'placeholder="Отключено" spellcheck="false"')}</div>
        <div class="awg-cps-generator">
          <button type="button" data-cps-open>Сгенерировать I1–I5</button>
          <div data-cps-panel hidden>
            <div class="grid-two">
              <label>Сколько заполнить<select name="cps_count">${[1, 2, 3, 4, 5].map((n) => `<option value="${n}">${n}</option>`).join("")}</select></label>
              <label>Профиль<select name="cps_profile"><option value="auto">Случайно</option><option value="dns">DNS</option><option value="stun">STUN</option><option value="quic">QUIC</option></select></label>
              <label data-cps-service hidden>Сервис<select name="cps_service"><option value="auto">Случайно</option><option value="google">Google</option><option value="apple">Apple</option><option value="microsoft">Microsoft</option><option value="custom">Свой домен</option></select></label>
              <label data-cps-domain hidden>Домен *<input name="cps_domain" autocomplete="off" spellcheck="false"></label>
            </div>
            <p data-cps-error role="alert" hidden></p>
            <div class="awg-cps-actions"><button type="button" data-cps-cancel>Отмена</button><button type="button" data-cps-generate>Сгенерировать</button></div>
          </div>
        </div>
      `)}
      ${section("Дополнительные настройки Amnezia", `
        <div class="grid-two">${timers.map((name) => {
          const value = String(config.timings?.[name] ?? "").trim();
          const label = name === "MaxHandshakeAttempts" ? `${name}, попыток` : `${name}, сек.`;
          return field(name, label, !value || value === "0" ? timingDefaults[name] : value, 'spellcheck="false"');
        }).join("")}</div>
        <div class="grid-two">${[["RandomTrailers",config.random_trailers], ["DisableCookies",config.disable_cookies]].map(([name,enabled]) => `<label>${name}<select name="${name}"><option value="on"${enabled ? " selected" : ""}>Включено</option><option value="off"${!enabled ? " selected" : ""}>Выключено</option></select></label>`).join("")}</div>
      `)}
      ${section("Дополнительные настройки", `<div class="grid-two">
        ${field("server_ipv4", "Адрес сервера в VPN", config.server_ipv4, 'required spellcheck="false"')}
        <label>Префикс подсети<select name="prefix">${Array.from({ length: 15 }, (_, i) => 16 + i).map((value) => `<option value="${value}"${String(value) === prefix ? " selected" : ""}>/${value}</option>`).join("")}</select></label>
        </div><div class="awg-network-preview">Подсеть VPN: <output name="network">${escape(config.client_ipv4_cidr)}</output></div>
        <div class="grid-two">${field("mtu", "MTU клиентов", config.mtu, 'type="number" min="1" max="65535"')}${field("persistent_keepalive", "PersistentKeepalive клиентов, сек.", config.persistent_keepalive, 'type="number" min="0" max="65535"')}</div>
      `)}
      ${section("Маршруты клиентов", `
        <label>IPv4<select name="route_mode"><option value="all"${allIPv4 ? " selected" : ""}>Весь трафик через VPN</option><option value="include"${!allIPv4 && !excludeIPv4 ? " selected" : ""}>Только указанные сети</option><option value="exclude"${excludeIPv4 ? " selected" : ""}>Весь трафик, кроме указанных сетей</option></select></label>
        <label data-route-networks${allIPv4 ? " hidden" : ""}><span data-route-label>Сети через VPN</span><textarea name="route_networks" rows="4" spellcheck="false" placeholder="10.0.0.0/8&#10;192.168.0.0/16">${escape(excludeIPv4 ? config.client_excluded_routes : routes.filter((route) => !route.includes(":") && route !== "0.0.0.0/0").join("\n"))}</textarea></label>
        <label>IPv6<select name="ipv6_mode"><option value="off">Не направлять через VPN</option><option value="all"${routes.includes("::/0") ? " selected" : ""}>Весь трафик через VPN</option><option value="include"${routes.some((route) => route.includes(":") && route !== "::/0") ? " selected" : ""}>Только указанные сети</option></select></label>
        <label data-ipv6-networks hidden>Сети IPv6 через VPN<textarea name="ipv6_networks" rows="3" spellcheck="false">${escape(routes.filter((route) => route.includes(":") && route !== "::/0").join("\n"))}</textarea></label>
      `)}
      <div data-settings-warning class="notice warn" hidden>После смены HeaderProtectionKey старые конфигурации перестанут работать. Обновите конфигурации на устройствах.<label class="checkbox-line"><input type="checkbox" name="confirm_key_change"> Подтверждаю смену ключа</label></div>
      <p data-settings-result role="status" hidden></p>
    </div>`;
  }

  function render(config) {return `<form id="awg-settings-form" class="laz-modal-fields">${renderFields(config)}</form>`;}

  function defaults() {
    return {endpoint_host:"",endpoint_port:51820,dns:"1.1.1.1, 1.0.0.1",server_ipv4:"10.8.1.1",client_ipv4_cidr:"10.8.1.0/24",mtu:1376,persistent_keepalive:25,client_route_allowed_ips:"0.0.0.0/0",jc:4,jmin:40,jmax:70,s1:32,s2:48,s3:16,s4:16,h1:"1",h2:"2",h3:"3",h4:"4",header_protection_key_set:true,content_padding_addition:"32-128",timings:{...timingDefaults}};
  }

  function open(config, name, generate, save, load) {
    const dialog = window.lazUI.openModal({
      id: "awg-settings-dialog", title: `Настройки AmneziaWG · ${name}`, wide: true,
      className: "awg-settings-dialog", body: render(config), initialFocus: '[name="endpoint_host"]',
      actions: '<button type="button" data-close-dialog>Закрыть</button><button type="submit" form="awg-settings-form" data-settings-save>Сохранить и применить</button>',
    });
    const form = dialog.querySelector("form");
    form.noValidate = true;
    bindSave(dialog, form, config, save, load);
    bindFields(dialog,form,config,generate);
    return dialog;
  }

  function bindFields(dialog,form,config,generate,options={}) {
    bindValidation(form, config.header_protection_key_set);
    bindHeaderKeyGenerator(dialog, form, options.install);
    bindGenerator(dialog, form, generate);
    if(options.install) form.querySelector("[data-header-key-status]").textContent="Будет создан";
    const updateRoutes = () => {
      const mode = form.elements.route_mode.value;
      form.querySelector("[data-route-networks]").hidden = mode === "all";
      form.querySelector("[data-route-label]").textContent = mode === "exclude" ? "Сети вне VPN" : "Сети через VPN";
      form.querySelector("[data-ipv6-networks]").hidden = form.elements.ipv6_mode.value !== "include";
    };
    form.elements.route_mode.addEventListener("change", updateRoutes);
    form.elements.ipv6_mode.addEventListener("change", updateRoutes);
    updateRoutes();
    const updateNetwork = () => {
      try {
        const address = form.elements.server_ipv4.value.trim();
        const prefix = form.elements.prefix.value;
        form.elements.network.value = `${window.ipaddr.IPv4.networkAddressFromCIDR(`${address}/${prefix}`).toString()}/${prefix}`;
      } catch (_) {
        form.elements.network.value = "Некорректный адрес";
      }
    };
    form.elements.server_ipv4.addEventListener("input", updateNetwork);
    form.elements.prefix.addEventListener("change", updateNetwork);
    updateNetwork();
  }

  function read(form, version=0) {
    const fields=form.elements;
    const body = {config_version_ms:version, timings:{}};
    for (const key of ["endpoint_host","dns","server_ipv4","content_padding_addition","route_mode","route_networks","ipv6_mode","ipv6_networks","h1","h2","h3","h4","i1","i2","i3","i4","i5"]) body[key]=fields[key].value.trim();
    for (const key of ["endpoint_port","prefix","mtu","persistent_keepalive","jc","jmin","jmax","s1","s2","s3","s4"]) body[key]=Number(fields[key].value);
    for (const key of ["RekeyAfterTime","RekeyTimeout","RejectAfterTime","KeepaliveTimeout","MaxHandshakeAttempts"]) body.timings[key]=fields[key].value.trim();
    body.random_trailers=fields.RandomTrailers.value==="on";body.disable_cookies=fields.DisableCookies.value==="on";
    if (!fields.header_protection_key.disabled) body.header_protection_key=fields.header_protection_key.value;
    return body;
  }

  function validate(form) {
    form.dispatchEvent(new Event("awg-draft-change"));
    if(form.checkValidity())return true;
    for(const input of form.querySelectorAll(":invalid")){const section=input.closest("details");if(section)section.open=true;input.dispatchEvent(new Event("focusout",{bubbles:true}));}
    form.reportValidity();return false;
  }

  function bindSave(dialog, form, initial, save, load) {
    const button = dialog.querySelector("[data-settings-save]");
    button.disabled = typeof save !== "function";
    const result = form.querySelector("[data-settings-result]");
    let config = initial;
    let pending = false;
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (pending || typeof save !== "function") return;
      if (!validate(form)) return;
      const fields = form.elements;
      if (!fields.header_protection_key.disabled && !fields.confirm_key_change.checked) {
        form.querySelector("[data-settings-warning]").hidden = false;
        fields.confirm_key_change.focus(); return;
      }
      const body = read(form,config.config_version_ms);
      pending=true;button.disabled=true;
      button.textContent="Применение…";form.inert=true;
      let saved=false;
      result.hidden=false;result.textContent="Сохранение…";
      try {
        const updated = await save(body);
        if (!updated) throw Error("Не удалось сохранить настройки");
        config = updated;
        saved=true;Object.assign(initial,config);
        fields.header_protection_key.value="";fields.header_protection_key.disabled=true;
        form.querySelector("[data-settings-warning]").hidden=true;fields.confirm_key_change.checked=false;
        form.querySelector("[data-header-key-status]").textContent=config.header_protection_key_set ? "Задан" : "Не задан";
        form.querySelector("[data-header-key-status]").className=config.header_protection_key_set ? "status active" : "status held";
        result.textContent="Настройки сохранены. Ожидается применение на сервере…";
        for (let i=0; i<60 && dialog.isConnected && typeof load==="function"; i++) {
          await new Promise(resolve=>setTimeout(resolve,1000));
          if (!dialog.isConnected) break;
          const state=await load();
          if (!state) throw Error("Требуется повторный вход");
          Object.assign(initial,state);
          if (state.config_version_ms!==config.config_version_ms) {result.textContent="Настройки изменены в другой сессии. Откройте форму заново.";break;}
          if (state.config_error_id) {result.textContent=`Настройки сохранены, но не применены. Ошибка: ${state.config_error_id}`;break;}
          if (state.applied_config_version_ms>=config.config_version_ms) {result.textContent="Настройки сохранены и применены.";break;}
          if (i===59) result.textContent="Настройки сохранены. Применение продолжится после восстановления связи с сервером.";
        }
      } catch (err) {result.textContent=(saved ? "Настройки сохранены. Проверить применение не удалось: " : "")+(err.message || "Не удалось сохранить настройки");}
      finally {pending=false;button.disabled=false;button.textContent="Сохранить и применить";form.inert=false;}
    });
  }

  function bindValidation(form, hasHeaderKey) {
    const root=form.querySelector("[data-awg-settings]")||form;
    const controls = [...root.querySelectorAll("input:not([type=hidden]), textarea, select")];
    for (const input of controls) {
      const error = document.createElement("small");
      error.id = `awg-error-${input.name}`;
      error.className = "awg-field-error";
      error.hidden = true;
      input.setAttribute("aria-describedby", error.id);
      input.after(error);
    }
    const touched = new Set();
    const validate = () => {
      const values = Object.fromEntries([...form.elements].filter((e) => e.name).map((e) => [e.name, e.value]));
      const errors = window.LazAmneziaValidation.validate(values, hasHeaderKey);
      for (const input of controls) {
        const error = form.querySelector(`#awg-error-${input.name}`);
        const message = errors[input.name] || (input.validity.badInput ? "Введите число" : "");
        input.setCustomValidity(message);
        const show = touched.has(input.name) && !!message;
        input.setAttribute("aria-invalid", String(show));
        error.textContent = show ? message : "";
        error.hidden = !show;
      }
    };
    form.addEventListener("focusout", (e) => { if (e.target.name) { touched.add(e.target.name); validate(); } });
    form.addEventListener("input", validate);
    form.addEventListener("change", validate);
    form.addEventListener("awg-draft-change", validate);
    validate();
  }

  function bindHeaderKeyGenerator(dialog, form, install=false) {
    const input = form.elements.header_protection_key;
    const status = form.querySelector("[data-header-key-status]");
    const error = form.querySelector("[data-header-key-error]");
    form.querySelector("[data-header-key-generate]").addEventListener("click", () => {
      try {
        const bytes = new Uint8Array(32);
        window.crypto.getRandomValues(bytes);
        const key = window.btoa(String.fromCharCode(...bytes));
        input.value = key;
        input.disabled = false;
        form.elements.confirm_key_change.checked=false;
        form.querySelector("[data-settings-warning]").hidden=install;
        form.dispatchEvent(new Event("awg-draft-change"));
        status.textContent = "Новый ключ — не сохранён";
        status.className = "status held";
        error.hidden = true;
        error.textContent = "";
      } catch (_) {
        error.textContent = "Не удалось создать ключ. Повторите попытку.";
        error.hidden = false;
      }
    });
    dialog.addEventListener("close", () => { input.value = ""; input.disabled = true; });
  }

  function bindGenerator(dialog, form, generate) {
    const find = (selector) => form.querySelector(selector);
    const panel = find("[data-cps-panel]");
    const opener = find("[data-cps-open]");
    const submit = find("[data-cps-generate]");
    const error = find("[data-cps-error]");
    const fields = form.elements;
    let controller;
    let generation = 0;
    const update = () => {
      const service = ["dns", "quic"].includes(fields.cps_profile.value);
      find("[data-cps-service]").hidden = !service;
      const domain = service && fields.cps_service.value === "custom";
      find("[data-cps-domain]").hidden = !domain;
      fields.cps_domain.required = domain;
      fields.cps_domain.disabled = !domain;
      form.dispatchEvent(new Event("awg-draft-change"));
    };
    const busy = (value) => {
      panel.setAttribute("aria-busy", String(value));
      panel.querySelectorAll("input, select").forEach((input) => { input.disabled = value; });
      for (let i = 1; i <= 5; i++) fields[`i${i}`].disabled = value;
      submit.disabled = value;
      submit.textContent = value ? "Генерация…" : "Сгенерировать";
      if (!value) update();
    };
    const reset = () => {
      generation++;
      controller?.abort();
      controller = null;
      fields.cps_count.value = "1";
      fields.cps_profile.value = "auto";
      fields.cps_service.value = "auto";
      fields.cps_domain.value = "";
      error.textContent = "";
      error.hidden = true;
      busy(false);
      panel.hidden = true;
      opener.hidden = false;
    };
    opener.addEventListener("click", () => {
      panel.hidden = false;
      opener.hidden = true;
      fields.cps_count.focus();
    });
    fields.cps_profile.addEventListener("change", update);
    fields.cps_service.addEventListener("change", update);
    find("[data-cps-cancel]").addEventListener("click", () => { reset(); opener.focus(); });
    dialog.addEventListener("close", reset);
    submit.addEventListener("click", async () => {
      if (controller) return;
      if (fields.cps_domain.required && !fields.cps_domain.reportValidity()) return;
      const count = Number(fields.cps_count.value);
      const options = { count, profile: fields.cps_profile.value };
      if (["dns", "quic"].includes(options.profile)) {
        options.service = fields.cps_service.value;
        if (options.service === "custom") options.server_name = fields.cps_domain.value.trim();
      }
      controller = new AbortController();
      const current = ++generation;
      error.hidden = true;
      busy(true);
      try {
        const result = await generate(options, controller.signal);
        if (current !== generation || !dialog.isConnected) return;
        if (!Array.isArray(result?.signatures) || result.signatures.length !== count || result.signatures.some((s) => typeof s !== "string" || !s)) {
          throw new Error("Не удалось получить сигнатуры");
        }
        for (let i = 1; i <= 5; i++) fields[`i${i}`].value = result.signatures[i - 1] || "";
        reset();
        opener.focus();
      } catch (err) {
        if (current !== generation || !dialog.isConnected) return;
        error.textContent = err.message || "Не удалось сгенерировать сигнатуры";
        error.hidden = false;
      } finally {
        if (current === generation) { controller = null; busy(false); }
      }
    });
    reset();
  }

  window.LazAmneziaSettings = { open, render, renderFields, bind:bindFields, read, validate, defaults };
})();
