const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "../../internal/server/web/admin/app.js"), "utf8");
const context = {window:{},Intl,document:{querySelector:selector=>({textContent:selector==="#hysteria-defaults"?JSON.stringify(require("./hysteria-fixture.cjs")):'["FI","NL","RU"]'})}};
vm.runInNewContext(fs.readFileSync(path.join(__dirname,"../../internal/server/web/admin/hysteria-settings.js"),"utf8"),context);
vm.runInNewContext(fs.readFileSync(path.join(__dirname,"../../internal/server/web/admin/amnezia-settings.js"),"utf8"),context);
vm.runInNewContext(
  source.slice(source.indexOf("function renderServerIdentityFields("), source.indexOf("function openAWGNodeDialog(")) +
  source.slice(source.indexOf("function renderHysteriaNodeDialogBody("), source.indexOf("function openHysteriaNodeDialog(")),
  context,
);

for (const [protocol, render] of [
  ["Hysteria2", () => context.renderHysteriaNodeDialogBody()],
  ["AmneziaWG", () => context.renderAWGNodeDialogBody()],
]) {
  test(protocol + ": common order and consistent SSH field names", () => {
    const body = render();
    const name = body.indexOf('name="node_name"');
    const ssh = body.indexOf("<legend>SSH-доступ к серверу</legend>");
    const settings = body.indexOf("<legend>Настройки " + protocol + "</legend>");
    assert.ok(name >= 0 && name < ssh && ssh < settings);
    assert.equal((body.match(/name="node_name"/g) || []).length, 1);
    assert.ok(body.includes("«Пользователь» и «Пароль»"));
    assert.ok(body.indexOf("«Пользователь»") > body.indexOf('name="bootstrap_password"'));
    assert.equal(body.includes("Bootstrap"), false);
    assert.equal(body.includes("VPS"), false);
    assert.equal(body.includes("По умолчанию"), false);
    assert.ok(body.includes('name="region_choice"'));
    assert.ok(body.includes('IP *<input name="ssh_host"'));
    assert.ok(body.includes(">Установить</button>"));
    for (const name of ["ssh_port", "bootstrap_user"]) {
      assert.ok(body.includes('name="' + name + '"'));
    }
  });
}

test("Hysteria2: required public domain uses an enabled-by-default auto checkbox", () => {
  const body = context.renderHysteriaNodeDialogBody();
  assert.match(body, /name="public_domain" readonly required autocomplete="off"/);
  assert.ok(body.includes('name="domain_auto" type="checkbox" checked'));
  assert.ok(body.includes('for="hysteria-public-domain">Публичный домен *</label>'));
  assert.equal(body.includes('name="domain_mode"'), false);
  assert.ok(body.includes('data-h2-path="masquerade.type"'));
  assert.ok(body.includes('name="hysteria_port" type="number" min="1" max="65535" value="443"'));
  for(const name of ["install_version","acme_email","obfs_enabled"])assert.equal(body.includes(`name="${name}"`),false);
  assert.equal(body.includes('name="traffic_stats_listen"'), false);
  assert.equal(body.includes('name="traffic_stats_enabled"'), false);
  assert.equal(body.includes("Адрес Traffic stats"), false);
});

test("AmneziaWG: supported subnet masks and defaults remain explicit", () => {
  const body = context.renderAWGNodeDialogBody();
  assert.ok(body.includes('name="endpoint_port" value="51820"'));
  assert.ok(body.includes('value="24" selected'));
  assert.equal((body.match(/name="prefix">([\s\S]*?)<\/select>/)[1].match(/<option /g) || []).length, 15);
  assert.ok(body.includes('name="allow_package_upgrade" type="checkbox" required'));
  for(const name of ["jc","s4","h4","i5","dns","mtu","RekeyTimeout","route_mode"])assert.ok(body.includes(`name="${name}"`));
  assert.equal((body.match(/<form\b/g)||[]).length,1);
});
