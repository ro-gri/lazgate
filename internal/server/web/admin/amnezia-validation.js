(function () {
  const uint32 = 4294967295;
  function range(value, max = uint32) {
    const match = /^(\d+)(?:-(\d+))?$/.exec(value);
    if (!match) return null;
    const low = Number(match[1]), high = Number(match[2] ?? match[1]);
    return low <= high && high <= max ? [low, high] : null;
  }
  function ip(value, version) {
    try {
      if (value.includes("%")) return false;
      const parsed = window.ipaddr.parse(value);
      if (parsed.kind() === "ipv4" && !window.ipaddr.IPv4.isValidFourPartDecimal(value)) return false;
      return !version || parsed.kind() === version;
    } catch (_) { return false; }
  }
  function domain(value) {
    if (!value || value.length > 253 || /^[\d.]+$/.test(value) || /[\s/:@?#%\\]/.test(value)) return false;
    try {
      const host = new URL(`https://${value}`).hostname.replace(/\.$/, "");
      return host.length <= 253 && host.split(".").every((label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label));
    } catch (_) { return false; }
  }
  function cpsError(value) {
    if (!value) return "";
    if (/[\r\n\x00]/.test(value)) return "CPS должна быть записана одной строкой";
    const tags = /<b 0x([0-9a-fA-F]+)>|<(r|rc|rd) (\d+)>|<t>/g;
    let end = 0, size = 0;
    for (const match of value.matchAll(tags)) {
      if (value.slice(end, match.index).trim()) return "Допустимы теги <b 0x…>, <r N>, <rc N>, <rd N>, <t>";
      if (match[1]) {
        if (match[1].length % 2) return "Hex-строка должна содержать чётное число символов";
        size += match[1].length / 2;
      } else if (match[2]) {
        const length = Number(match[3]);
        if (!Number.isSafeInteger(length) || length < 1) return "Размер случайных данных должен быть положительным целым числом";
        size += length;
      } else size += 4;
      end = match.index + match[0].length;
    }
    if (!end || value.slice(end).trim()) return "Некорректная CPS-строка";
    return size > 65507 ? "Размер UDP-пакета не должен превышать 65507 байт" : "";
  }
  function validate(values, hasHeaderKey = false) {
    const errors = {};
    const text = (name) => String(values[name] ?? "").trim();
    const fail = (name, message) => { if (!errors[name]) errors[name] = message; };
    const number = (name, min, max, required = false) => {
      const value = text(name);
      if (!value && !required) return;
      if (!/^\d+$/.test(value) || Number(value) < min || Number(value) > max) fail(name, `Целое число от ${min} до ${max}`);
    };
    if (!ip(text("endpoint_host")) && !domain(text("endpoint_host"))) fail("endpoint_host", "Введите IP или доменное имя без схемы, порта и пути");
    number("endpoint_port", 1, 65535, true);
    if (text("dns") && text("dns").split(/[,\s]+/).some((s) => !ip(s))) fail("dns", "Укажите IP-адреса DNS через запятую или пробел");
    for (const key of ["jc", "jmin", "jmax"]) number(key, 0, 65535);
    if (!errors.jmin && !errors.jmax && Number(text("jmin")) > Number(text("jmax"))) fail("jmax", "Jmax должен быть не меньше Jmin");
    for (let i = 1; i <= 4; i++) number(`s${i}`, hasHeaderKey || text("header_protection_key") ? 12 : 0, 65535, hasHeaderKey || !!text("header_protection_key"));
    const headers = [];
    for (let i = 1; i <= 4; i++) {
      const name = `h${i}`, value = range(text(name) || "0");
      if (!value) fail(name, `Число или диапазон от 0 до ${uint32}, начало не больше конца`);
      else headers.push({ name, value });
    }
    for (const a of headers) for (const b of headers) {
      if (a !== b && a.value[0] <= b.value[1] && b.value[0] <= a.value[1]) fail(a.name, "Диапазоны H1–H4 не должны пересекаться");
    }
    for (const name of ["content_padding_addition", "RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts"]) {
      const value = text(name);
      if (value && value !== "(off)" && !range(value, 65535)) fail(name, "Число или диапазон от 0 до 65535, либо (off)");
    }
    for (let i = 1; i <= 5; i++) {
      const error = cpsError(text(`i${i}`));
      if (error) fail(`i${i}`, error);
    }
    if (!ip(text("server_ipv4"), "ipv4")) fail("server_ipv4", "Введите IPv4-адрес сервера в VPN");
    number("prefix", 16, 30, true);
    if (!errors.server_ipv4 && !errors.prefix) {
      const address = window.ipaddr.parse(text("server_ipv4")).toByteArray().reduce((n, b) => n * 256 + b, 0);
      const block = 2 ** (32 - Number(text("prefix")));
      if (address % block === 0 || address % block === block - 1) fail("server_ipv4", "Адрес сервера не может быть адресом сети или broadcast");
    }
    number("mtu", 68, 65535);
    number("persistent_keepalive", 0, 65535);
    for (const [name, enabled, kind] of [["route_networks", text("route_mode") !== "all", "ipv4"], ["ipv6_networks", text("ipv6_mode") === "include", "ipv6"]]) {
      if (!enabled) continue;
      const networks = text(name).split(/[,\s]+/).filter(Boolean);
      if (!networks.length) { fail(name, "Укажите хотя бы одну сеть в формате CIDR"); continue; }
      for (const network of networks) {
        try {
          const [address, prefix] = network.split("/");
          if (network.split("/").length !== 2 || !ip(address, kind) || !/^\d+$/.test(prefix)) throw Error();
          window.ipaddr.parseCIDR(network);
        } catch (_) { fail(name, `Некорректная сеть ${kind === "ipv4" ? "IPv4" : "IPv6"}: ${network}`); break; }
      }
    }
    if (["dns", "quic"].includes(text("cps_profile")) && text("cps_service") === "custom" && (!domain(text("cps_domain")) || ip(text("cps_domain")))) fail("cps_domain", "Введите доменное имя без схемы, порта и пути (не IP)");
    return errors;
  }
  window.LazAmneziaValidation = { validate, cpsError };
})();
