const {test}=require("node:test");
const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const path=require("node:path");
const fixture=require("./hysteria-fixture.cjs");
const context={window:{},Intl,TextEncoder,URL,document:{querySelector:selector=>({textContent:selector==="#hysteria-defaults"?JSON.stringify(fixture):'["FI","NL","RU"]'})}};
vm.runInNewContext(fs.readFileSync(path.join(__dirname,"../../internal/server/web/admin/shakespeare-sonnets.js"),"utf8"),context);
vm.runInNewContext(fs.readFileSync(path.join(__dirname,"../../internal/server/web/admin/hysteria-settings.js"),"utf8"),context);
const settings=context.window.LazHysteriaSettings;
test("String masquerade has a multiline editor and four client-side modes",()=>{
 const c=settings.defaults();
 const html=settings.renderFields({settings:c});
 assert.match(html,/<textarea data-h2-path="masquerade.content" data-h2-format="text" rows="12"/);
 for(const title of ["Простой текст","HTML","Вручную","Автоматически","Другой сонет"])assert.ok(html.includes(title));
 const sonnets=context.window.LazShakespeareSonnets;
 assert.equal(sonnets.length,154);assert.equal(new Set(sonnets).size,154);
 for(const sonnet of sonnets){assert.match(sonnet,/^Sonnet [IVXLCDM]+\nWilliam Shakespeare\n\n/);assert.ok(sonnet.length>300)}
 c.masquerade.content=sonnets[0];c.masquerade.headers={"Content-Type":"text/plain; charset=utf-8"};
 assert.match(settings.renderFields({settings:c}),/value="auto" selected/);
 c.masquerade.statusCode=404;
 assert.match(settings.renderFields({settings:c}),/value="manual" selected/);
});
test("String modes preserve multiline text and edited status and headers on save",()=>{
 for(const mode of ["text","html","auto","manual"]){
  const initial={settings:settings.defaults()};initial.settings.masquerade.type="string";
  const content=mode==="auto"?context.window.LazShakespeareSonnets[0]:"  First\nSecond\n";
  const fields=[{type:"textarea",value:content,dataset:{h2Path:"masquerade.content",h2Format:"text"}},{type:"textarea",value:'{"X-Test":"yes"}',dataset:{h2Path:"masquerade.headers",h2Format:"json"}},{type:"number",value:"201",dataset:{h2Path:"masquerade.statusCode"}}];
  const form={elements:{public_domain:{value:"h2.example.com"},hysteria_port:{value:"443"}},querySelectorAll:selector=>selector==="[data-h2-path]"?fields:[],querySelector:selector=>selector==="[data-h2-string-mode]"?{value:mode}:{selectedOptions:[]}};
  const result=settings.read(form,initial).settings.masquerade;
  assert.equal(result.content,content);
  assert.equal(result.statusCode,201);
  assert.equal(JSON.stringify(result.headers),JSON.stringify({"X-Test":"yes"}));
 }
});
test("Hysteria form has explicit settings without LazGate or ACME controls",()=>{
 const html=settings.renderFields({endpoint_host:"h2.example.com",endpoint_port:443,settings:settings.defaults()});
 for(const path of ["obfs.type","obfs.password","quic.maxIdleTimeout","resolver.type","protect_internal","masquerade.type","bandwidth.up","outbounds.0.mode","sniff.enable"])assert.ok(html.includes(`data-h2-path="${path}"`),path);
 assert.match(html,/data-h2-path="protect_internal" checked/);
 assert.match(html,/value="RU"/);
 assert.equal(html.includes('value="RU" selected'),false);
 assert.ok(html.includes('value="30s"'));
 assert.ok(html.includes('value="60s"'));
 for(const value of ["trafficStats","auth.http","acme_email","Стандартное поведение"])assert.equal(html.includes(value),false);
 assert.equal(html.includes("<form"),false);
 for(const selector of ["data-h2-upload-files","data-h2-upload-zip","data-h2-file-list"])assert.ok(html.includes(selector));
 assert.ok(html.includes('type="hidden" data-h2-path="masquerade.dir"'));
 assert.equal(html.includes("Каталог файлов на сервере"),false);
});
test("Hysteria client validation accepts defaults and rejects invalid cross-field values",()=>{
 assert.equal(settings.validateSettings(settings.defaults()),true);
 for(const mutate of [c=>c.quic.maxIdleTimeout="1s",c=>c.quic.maxStreamReceiveWindow=1,c=>c.obfs.type="gecko",c=>c.resolver.type="tls",c=>c.bandwidth.up="1.5 mbps",c=>c.sniff.tcpPorts="443-80",c=>c.outbounds.push({...c.outbounds[0]})]){const c=settings.defaults();mutate(c);assert.throws(()=>settings.validateSettings(c))}
 const c=settings.defaults();c.masquerade.type="string";c.masquerade.listenHTTP=":80";assert.throws(()=>settings.validateSettings(c));c.masquerade.listenHTTPS=":443";assert.equal(settings.validateSettings(c),true);
});
test("Hysteria form preserves existing explicit settings and safely escapes values",()=>{
 const c=settings.defaults();c.quic.maxIdleTimeout="20s";c.blocked_countries=["FI"];c.masquerade.content='<img src=x onerror="bad()">';
 const html=settings.renderFields({endpoint_host:"h2.example.com",endpoint_port:8443,obfs_password_set:true,settings:c});
 assert.ok(html.includes('value="20s"'));
 assert.ok(html.includes('value="FI" selected'));
 assert.ok(html.includes('value="8443"'));
 assert.equal(html.includes("<img"),false);
 assert.ok(html.includes("Пароль задан"));
 const install=settings.renderFields({settings:c},{install:true});assert.equal(install.includes('name="public_domain"'),false);
});
