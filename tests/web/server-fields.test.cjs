const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ipaddr = require("../../internal/server/web/admin/ipaddr.js");
const context = {
  window: {}, ipaddr, Intl,
  document: { querySelector: () => ({ textContent: '["FI","NL","GB"]' }) },
};
vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/server-fields.js"), "utf8"), context);
const parse = context.window.lazServerFields.parseIP;

test("IP fields reject hostnames, ports, zones and legacy IPv4 forms", () => {
  for (const value of ["", "node.example", "https://1.1.1.1", "1.1.1.1:22", "127.1", "010.0.0.1", "fe80::1%eth0", "[::1]"]) {
    assert.equal(parse(value), null, value);
  }
});

test("IP fields normalize IPv4, IPv6 and mapped IPv4", () => {
  for (const [value, expected] of [
    [" 104.128.142.63 ", "104.128.142.63"],
    ["2001:0db8:0:0:0:0:0:1", "2001:db8::1"],
    ["::ffff:104.128.142.63", "104.128.142.63"],
  ]) assert.equal(parse(value).toString(), expected);
});
