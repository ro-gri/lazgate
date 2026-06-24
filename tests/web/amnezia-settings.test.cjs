const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/amnezia-settings.js"), "utf8");
const context = { window: {} };
vm.runInNewContext(source, context);
const config = {endpoint_host:"vpn.example.com",endpoint_port:51820,dns:"1.1.1.1, 1.0.0.1",server_ipv4:"10.8.1.1",client_ipv4_cidr:"10.8.1.0/24",mtu:1280,persistent_keepalive:25,client_route_allowed_ips:"0.0.0.0/0",jc:5,jmin:48,jmax:272,s1:124,s2:16,s3:23,s4:20,h1:1,h2:2,h3:3,h4:4,i1:0,i2:0,i3:0,i4:0,i5:0,header_protection_key_set:true,content_padding_addition:"32-128"};

test("settings form is populated and grouped without secret fields", () => {
  const html = context.window.LazAmneziaSettings.render(config);
  for (const title of ["Основные настройки", "Основные настройки Amnezia", "Дополнительные настройки Amnezia", "Дополнительные настройки", "Маршруты клиентов"]) assert.ok(html.includes(title));
  for (const value of ["vpn.example.com", "51820", "1280", "32-128", 'value="24" selected']) assert.ok(html.includes(value));
  assert.ok(html.includes('name="i1" value=""'));
  assert.match(html, /type="hidden" name="header_protection_key" value="" disabled/);
  assert.equal(html.includes('name="private_key"'), false);
  assert.match(html, /type="button" data-header-key-generate>Перегенерировать/);
  assert.match(html, /href="https:\/\/github.com\/amnezia-vpn\/amneziawg-go#configuration" target="_blank" rel="noopener noreferrer"/);
});

test("partial routes and unsafe input render correctly", () => {
  const html = context.window.LazAmneziaSettings.render({...config,endpoint_host:'"><script>bad</script>',client_route_allowed_ips:"10.0.0.0/8, ::/0"});
  assert.ok(html.includes('value="include" selected'));
  assert.ok(html.includes("10.0.0.0/8"));
  assert.ok(html.includes('value="all" selected'));
  assert.equal(html.includes("<script>bad</script>"), false);
});

test("timings show explicit native defaults and preserve overrides", () => {
  const settings = context.window.LazAmneziaSettings;
  const expected = {RekeyAfterTime:"120", RekeyTimeout:"5", RejectAfterTime:"180", KeepaliveTimeout:"10", MaxHandshakeAttempts:"18"};
  for (const timings of [undefined, {}, {RekeyTimeout:"", RejectAfterTime:"0"}]) {
    const html = settings.render({...config, timings});
    assert.equal(html.includes("Стандартное поведение"), false);
    for (const [name, value] of Object.entries(expected)) {
      assert.ok(html.includes(`name="${name}" value="${value}"`));
      assert.equal(settings.defaults().timings[name], value);
    }
    assert.ok(html.includes("RekeyTimeout, сек."));
    assert.ok(html.includes("MaxHandshakeAttempts, попыток"));
  }
  assert.ok(settings.render({...config, timings:{RekeyTimeout:"7-10"}}).includes('name="RekeyTimeout" value="7-10"'));
});

test("CPS generator is an inline panel after I5 with count and dependent options", () => {
  const html = context.window.LazAmneziaSettings.render(config);
  assert.ok(html.indexOf('data-cps-open') > html.indexOf('name="i5"'));
  assert.match(html, /data-cps-panel hidden/);
  for (const name of ["cps_count", "cps_profile", "cps_service", "cps_domain"]) assert.ok(html.includes(`name="${name}"`));
  for (const profile of ["auto", "dns", "stun", "quic"]) assert.ok(html.includes(`value="${profile}"`));
  assert.match(html, /data-cps-error role="alert" hidden/);
  assert.equal(html.includes("<dialog"), false);
});
