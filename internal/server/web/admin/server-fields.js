/* Shared IP, informational country and automatic endpoint fields. */
window.lazServerFields = (() => {
  const codes = JSON.parse(document.querySelector("#country-codes")?.textContent || "[]");
  const countries = new Set(codes);
  const names = new Intl.DisplayNames(["ru"], { type: "region" });
  const cache = new Map();
  const flag = (code) => String.fromCodePoint(...Array.from(code, (letter) => 127397 + letter.charCodeAt(0)));
  const label = (code) => names.of(code) + " " + flag(code);

  function parseIP(value) {
    const input = String(value || "").trim();
    if (!input || input.includes("%") || input.includes("[") || input.includes("]")) return null;
    if (!input.includes(":") && !ipaddr.IPv4.isValidFourPartDecimal(input)) return null;
    try {
      let address = ipaddr.parse(input);
      if (address.kind() === "ipv6" && address.isIPv4MappedAddress()) address = address.toIPv4Address();
      return address;
    } catch (_) {
      return null;
    }
  }

  class Fields {
    constructor(form) {
      this.form = form;
      this.ip = form.elements.ssh_host;
      this.choice = form.elements.region_choice;
      this.region = form.elements.region;
      this.domain = form.elements.public_domain;
      this.domainAuto = form.elements.domain_auto;
      this.endpoint = form.elements.endpoint_host;
      this.endpointEdited = false;
      this.manualDomain = null;
      this.sequence = 0;
      this.geoIP = "";
      this.promise = null;
      this.timer = null;
      this.abort = null;
      this.disposed = false;
      codes.map((code) => ({ code, text: label(code) }))
        .sort((a, b) => a.text.localeCompare(b.text, "ru"))
        .forEach(({ code, text }) => this.choice.add(new Option(text, code)));
      this.ip.addEventListener("input", () => this.changed());
      this.ip.addEventListener("blur", () => {
        const address = parseIP(this.ip.value);
        if (address) this.ip.value = address.toString();
      });
      this.choice.addEventListener("change", () => {
        this.cancel();
        this.region.value = this.choice.value === "auto" ? "" : this.choice.value;
        if (this.choice.value === "auto") void this.lookup();
      });
      this.domainAuto?.addEventListener("change", () => {
        if (this.domainAuto.checked) this.manualDomain = this.domain.value;
        this.updateDomain(true);
      });
      this.endpoint?.addEventListener("input", () => {
        this.endpointEdited = this.endpoint.value !== "";
        this.updateEndpoint();
      });
      form.closest("dialog")?.addEventListener("close", () => {
        this.disposed = true;
        this.cancel();
      }, { once: true });
      this.changed();
    }

    cancel() {
      this.sequence++;
      clearTimeout(this.timer);
      this.abort?.abort();
      this.promise = null;
      this.geoIP = "";
    }

    changed() {
      const address = parseIP(this.ip.value);
      this.ip.setCustomValidity(this.ip.value && !address ? "Укажите IP-адрес без домена, порта и зоны." : "");
      this.updateDomain();
      this.updateEndpoint();
      this.cancel();
      if (this.choice.value !== "auto") return;
      this.region.value = "";
      this.choice.options[0].textContent = "Автоматически";
      if (address) this.timer = setTimeout(() => { void this.lookup(); }, 450);
    }

    updateDomain(switched = false) {
      if (!this.domain) return;
      const auto = this.domainAuto.checked;
      const address = parseIP(this.ip.value);
      this.domain.readOnly = auto;
      this.domain.required = true;
      if (auto) {
        this.domain.value = address?.kind() === "ipv4"
          ? "h2." + address.toString().replaceAll(".", "-") + ".sslip.io" : "";
      } else if (switched && this.manualDomain !== null) {
        this.domain.value = this.manualDomain;
      }
      this.domainAuto.setCustomValidity(auto && address?.kind() === "ipv6"
        ? "Для IPv6 выберите ручной ввод публичного домена." : "");
    }

    updateEndpoint() {
      if (!this.endpoint) return;
      const address = parseIP(this.ip.value);
      if (!this.endpointEdited) this.endpoint.value = address?.kind() === "ipv4" ? address.toString() : "";
      this.endpoint.setCustomValidity(address?.kind() === "ipv6" && !this.endpoint.value
        ? "Укажите публичный IPv4-адрес или домен для подключения клиентов." : "");
    }

    lookup() {
      const address = parseIP(this.ip.value);
      if (this.choice.value !== "auto" || !address || this.disposed) return Promise.resolve();
      const ip = address.toString();
      if (this.geoIP === ip && this.promise) return this.promise;
      this.cancel();
      this.geoIP = ip;
      const sequence = this.sequence;
      const apply = (code) => {
        if (sequence !== this.sequence || this.choice.value !== "auto" || this.disposed) return;
        this.region.value = code;
        this.choice.options[0].textContent = code ? label(code) + " (auto)" : "Не определён (auto)";
      };
      if (cache.has(ip)) {
        apply(cache.get(ip));
        return Promise.resolve();
      }
      if (address.range() !== "unicast") {
        apply("");
        return Promise.resolve();
      }
      this.choice.options[0].textContent = "Определяем страну… (auto)";
      const controller = new AbortController();
      this.abort = controller;
      const timeout = setTimeout(() => controller.abort(), 4000);
      this.promise = (async () => {
        try {
          const response = await fetch("https://ipwho.is/" + encodeURIComponent(ip) + "?fields=success,country_code", {
            credentials: "omit", referrerPolicy: "no-referrer", signal: controller.signal,
          });
          if (!response.ok) throw new Error("Geo lookup failed");
          const result = await response.json();
          const code = String(result.country_code || "").toUpperCase();
          if (!result.success || !countries.has(code)) throw new Error("Unknown country");
          cache.set(ip, code);
          apply(code);
        } catch (_) {
          apply("");
        } finally {
          clearTimeout(timeout);
        }
      })();
      return this.promise;
    }

    async prepare() {
      clearTimeout(this.timer);
      const address = parseIP(this.ip.value);
      this.ip.setCustomValidity(address ? "" : "Укажите IP-адрес без домена, порта и зоны.");
      if (address) this.ip.value = address.toString();
      this.updateDomain();
      this.updateEndpoint();
      if (!this.form.reportValidity()) return false;
      if (this.choice.value === "auto") await this.lookup();
      return !this.disposed;
    }
  }

  return { bind: (form) => new Fields(form), parseIP };
})();
