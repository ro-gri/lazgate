(function () {
  const managedFilesRoot="/var/lib/lazgate-hysteria-files";
  const escape = value => String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
  const clone = value => JSON.parse(JSON.stringify(value));
  const defaults = () => clone(JSON.parse(document.querySelector("#hysteria-defaults").textContent));
  const get = (value, path) => path.split(".").reduce((v, k) => v?.[k], value);
  function set(value, path, next) { const keys = path.split("."); const key = keys.pop(); let target = value; for (const k of keys) target = target[k]; target[key] = next; }
  const input = (c, path, label, type="text", attrs="") => `<label>${escape(label)}<input data-h2-path="${path}" type="${type}" value="${escape(get(c,path))}" ${type === "number" ? 'step="1"' : ''} ${attrs} autocomplete="off" spellcheck="false"></label>`;
  const checkbox = (c,path,label) => `<label class="checkbox-line"><input type="checkbox" data-h2-path="${path}"${get(c,path)?" checked":""}>${escape(label)}</label>`;
  const select = (c,path,label,items) => `<label>${escape(label)}<select data-h2-path="${path}">${items.map(([value,title])=>`<option value="${value}"${String(get(c,path))===value?" selected":""}>${title}</option>`).join("")}</select></label>`;
  const text = (c,path,label,json=false) => `<label>${escape(label)}<textarea data-h2-path="${path}" data-h2-format="${json?"json":"lines"}" rows="4" spellcheck="false">${escape(json?JSON.stringify(get(c,path),null,2):Array.isArray(get(c,path))?get(c,path).join("\n"):get(c,path))}</textarea></label>`;
  const section = (title, body, open=false) => `<details class="awg-settings-section"${open?" open":""}><summary>${title}</summary><div class="awg-settings-content">${body}</div></details>`;

  function stringFields(m) {
    const headers=m.headers||{}, keys=Object.keys(headers);
    const contentType=keys.length===1&&keys[0].toLowerCase()==="content-type"?headers[keys[0]].toLowerCase():"";
    let mode="manual";
    if(m.statusCode===200){
      if((!keys.length&&!m.content)||contentType==="text/plain; charset=utf-8")mode="text";
      if(contentType==="text/html; charset=utf-8")mode="html";
    }
    if(mode==="text"&&window.LazShakespeareSonnets?.includes(m.content))mode="auto";
    return `<label>Содержимое<select data-h2-string-mode>${[["text","Простой текст"],["html","HTML"],["manual","Вручную"],["auto","Автоматически"]].map(([value,title])=>`<option value="${value}"${mode===value?" selected":""}>${title}</option>`).join("")}</select></label>
      <button type="button" data-h2-sonnet hidden>Другой сонет</button>
      <label>Содержимое ответа<textarea data-h2-path="masquerade.content" data-h2-format="text" rows="12" spellcheck="false">${escape(m.content)}</textarea></label>
      ${input({masquerade:m},"masquerade.statusCode","HTTP-код","number",'min="200" max="599"')}${text({masquerade:m},"masquerade.headers","HTTP-заголовки (JSON)",true)}`;
  }

  function bindString(form) {
    const mode=form.querySelector("[data-h2-string-mode]"),content=form.querySelector('[data-h2-path="masquerade.content"]');
    const button=form.querySelector("[data-h2-sonnet]");
    const generate=()=>{
      const sonnets=window.LazShakespeareSonnets;
      if(!sonnets?.length){error(form,"Не удалось загрузить набор сонетов. Перезагрузите страницу.");return}
      const choices=sonnets.filter(s=>s!==content.value);
      content.value=choices[crypto.getRandomValues(new Uint32Array(1))[0]%choices.length];
    };
    const update=()=>{button.hidden=mode.value!=="auto"};
    mode.addEventListener("change",()=>{
      if(mode.value!=="manual"){
        form.querySelector('[data-h2-path="masquerade.statusCode"]').value="200";
        form.querySelector('[data-h2-path="masquerade.headers"]').value=JSON.stringify({"Content-Type":mode.value==="html"?"text/html; charset=utf-8":"text/plain; charset=utf-8"},null,2);
      }
      if(mode.value==="auto")generate();update();
    });
    button.addEventListener("click",generate);update();
  }

  function fileFields(value) {
    return `<div class="h2-file-upload"><div class="connect-actions"><button type="button" data-h2-upload-files>Загрузить файлы</button><button type="button" data-h2-upload-zip>Загрузить ZIP</button></div>
      <input type="file" data-h2-pick-files multiple hidden><input type="file" data-h2-pick-zip accept=".zip,application/zip" hidden>
      <p class="helper">Новая загрузка заменяет весь набор после сохранения. До 128 файлов, 4 МиБ на файл, 8 МиБ суммарно и 2 МиБ в ZIP.</p>
      <p class="helper">Файлы будут публично доступны. Не загружайте пароли, ключи и другие секреты.</p>
      <input type="hidden" data-h2-path="masquerade.dir" value="${escape(value.settings.masquerade.dir)}">
      ${!value.file_set_id&&value.settings.masquerade.dir?`<p class="helper">Текущий каталог: ${escape(value.settings.masquerade.dir)}. Загрузка заменит его управляемым набором.</p>`:""}
      <div data-h2-file-list></div><p data-h2-file-status role="status" hidden></p></div>`;
  }

  function countries(c) {
    const codes=JSON.parse(document.querySelector("#country-codes").textContent), names=new Intl.DisplayNames(["ru"],{type:"region"});
    return `<label>Запретить доступ к ресурсам стран<select data-h2-countries multiple size="7">${codes.map(code=>({code,label:names.of(code)+" "+String.fromCodePoint(...Array.from(code,c=>127397+c.charCodeAt(0)))})).sort((a,b)=>a.label.localeCompare(b.label,"ru")).map(({code,label})=>`<option value="${code}"${(c.blocked_countries||[]).some(v=>v.toUpperCase()===code)?" selected":""}>${escape(label)}</option>`).join("")}</select></label>`;
  }
  function outbound(c,o,i) {
    const p=`outbounds.${i}.`;
    return `<fieldset class="node-install-settings" data-h2-outbound><legend>Выход ${i+1}</legend><div class="grid-two">${input(c,p+"name","Имя", "text","required")}${select(c,p+"type","Тип",[["direct","Напрямую"],["socks5","SOCKS5"],["http","HTTP-прокси"]])}</div>
      <div data-h2-when="${p}type=direct">${select(c,p+"mode","IP-режим",[["auto","Автоматически"],["4","Только IPv4"],["6","Только IPv6"],["46","Сначала IPv4"],["64","Сначала IPv6"]])}<div class="grid-two">${input(c,p+"bindIPv4","Исходящий IPv4")}${input(c,p+"bindIPv6","Исходящий IPv6")}${input(c,p+"bindDevice","Сетевой интерфейс")}</div>${checkbox(c,p+"fastOpen","TCP Fast Open")}</div>
      <div data-h2-when="${p}type=socks5">${input(c,p+"addr","Адрес:порт")}<div class="grid-two">${input(c,p+"username","Пользователь")}${input(c,p+"password","Пароль (пусто — сохранить)","password")}</div></div>
      <div data-h2-when="${p}type=http">${input(c,p+"url","URL прокси","url")}${checkbox(c,p+"insecure","Не проверять сертификат прокси")}</div>
      <button type="button" data-h2-remove-outbound="${i}">Удалить выход</button></fieldset>`;
  }
  function renderFields(value, options={}) {
    const c=value.settings, q=c.quic, m=c.masquerade;
    const endpoint=options.install?"":`<div class="grid-two"><label>Публичный домен *<input name="public_domain" value="${escape(value.endpoint_host)}" required autocomplete="off" spellcheck="false"></label><label>Публичный порт *<input name="hysteria_port" type="number" min="1" max="65535" value="${value.endpoint_port}" required></label></div>`;
    return `<div data-h2-settings class="laz-modal-fields"><a href="https://v2.hysteria.network/docs/advanced/Full-Server-Config/" target="_blank" rel="noopener noreferrer">Описание параметров Hysteria</a>
      ${section("Основные настройки",`${endpoint}<fieldset class="node-install-settings"><legend>ACL-защита</legend>${checkbox(c,"protect_internal","Защитить внутренние ресурсы")}${countries(c)}</fieldset>`,true)}
      ${section("Обфускация",`${select(c,"obfs.type","Тип обфускации",[["off","Выключена"],["salamander","Salamander"],["gecko","Gecko (экспериментально)"]])}<div data-h2-when="obfs.type=!off">${input(c,"obfs.password",value.obfs_password_set?"Пароль задан (пусто — сохранить)":"Пароль *","password")}<button type="button" data-h2-password>Перегенерировать пароль</button></div><div data-h2-when="obfs.type=gecko" class="grid-two">${input(c,"obfs.minPacketSize","Минимальный пакет, байт","number",'min="1" max="2048"')}${input(c,"obfs.maxPacketSize","Максимальный пакет, байт","number",'min="1" max="2048"')}</div>`)}
      ${section("Маскировка",`${select(c,"masquerade.type","Режим",[["off","Выключена"],["proxy","Proxy"],["file","File"],["string","String"]])}<div data-h2-when="masquerade.type=proxy">${input(c,"masquerade.url","Сайт маскировки","url")}${checkbox(c,"masquerade.rewriteHost","Подменять Host")}${checkbox(c,"masquerade.insecure","Не проверять сертификат сайта")}${checkbox(c,"masquerade.xForwarded","Передавать X-Forwarded")}</div><div data-h2-when="masquerade.type=file">${fileFields(value)}</div><div data-h2-when="masquerade.type=string">${stringFields(m)}</div><div data-h2-when="masquerade.type=!off"><div class="grid-two">${input(c,"masquerade.listenHTTP","HTTP адрес:порт")}${input(c,"masquerade.listenHTTPS","HTTPS адрес:порт")}</div>${checkbox(c,"masquerade.forceHTTPS","Перенаправлять HTTP на HTTPS")}</div>`)}
      ${section("Управление скоростью",`<div class="grid-two">${input(c,"bandwidth.up","Отправка сервера, например 200 mbps; 0 — без предела")}${input(c,"bandwidth.down","Приём сервера, например 200 mbps; 0 — без предела")}</div>${checkbox(c,"ignoreClientBandwidth","Игнорировать скорость клиента")}${checkbox(c,"bandwidth.disableLossCompensation","Отключить компенсацию потерь Brutal")}<div class="grid-two">${select(c,"congestion.type","Алгоритм",[["bbr","BBR"],["reno","Reno"]])}${select(c,"congestion.bbrProfile","Профиль BBR",[["standard","Standard"],["conservative","Conservative"],["aggressive","Aggressive"]])}</div>`)}
      ${section("Дополнительные настройки QUIC",`<div class="grid-two">${[["initStreamReceiveWindow","Начальное окно потока, байт"],["maxStreamReceiveWindow","Максимальное окно потока, байт"],["initConnReceiveWindow","Начальное окно соединения, байт"],["maxConnReceiveWindow","Максимальное окно соединения, байт"]].map(([key,label])=>input(c,"quic."+key,label,"number",'min="16384" required')).join("")}${input(c,"quic.maxIdleTimeout","Таймаут неактивности, например 30s","text","required")}${input(c,"quic.maxIncomingStreams","Максимум потоков","number",'min="8" required')}</div>${checkbox(c,"quic.disablePathMTUDiscovery","Отключить определение MTU")}${checkbox(c,"quic.disableStatelessReset","Отключить stateless reset")}`)}
      ${section("DNS",`${select(c,"resolver.type","DNS resolver",[["system","Системный"],["udp","UDP"],["tcp","TCP"],["tls","TLS"],["https","HTTPS"]])}<div data-h2-when="resolver.type=!system"><div class="grid-two">${input(c,"resolver.addr","Адрес:порт")}${input(c,"resolver.timeout","Таймаут, например 10s")}${input(c,"resolver.sni","SNI")}</div>${checkbox(c,"resolver.insecure","Не проверять сертификат DNS")}</div>`)}
      ${section("UDP и определение доменов",`${checkbox(c,"disableUDP","Запретить проксирование UDP")}${input(c,"udpIdleTimeout","Таймаут UDP-сессии, например 60s","text","required")}${checkbox(c,"sniff.enable","Определять домен (sniffing)")}<div data-h2-when="sniff.enable=true">${input(c,"sniff.timeout","Таймаут sniffing, например 2s")}${checkbox(c,"sniff.rewriteDomain","Переопределять домен запроса")}<div class="grid-two">${input(c,"sniff.tcpPorts","TCP-порты: all или 80,443,8000-9000")}${input(c,"sniff.udpPorts","UDP-порты")}</div></div>${checkbox(c,"speedTest","Встроенный тест скорости")}`)}
      ${section("Маршрутизация и дополнительные ACL",`${text(c,"acl_rules","Дополнительные правила ACL")}<div data-h2-outbounds>${c.outbounds.map((o,i)=>outbound(c,o,i)).join("")}</div><button type="button" data-h2-add-outbound>Добавить выход</button>`)}
      <p class="notice warn" data-h2-warning hidden>При смене домена, порта или обфускации обновите конфигурации на устройствах. Применение перезапустит Hysteria и прервёт текущие соединения.</p><p data-h2-result role="status" hidden></p></div>`;
  }

  function read(form,initial) {
    const body=clone(initial);
    body.settings.outbounds=[...form.querySelectorAll("[data-h2-outbound]")].map(()=>({}));
    for(const field of form.querySelectorAll("[data-h2-path]")) {
      let value=field.type==="checkbox"?field.checked:field.type==="number"?Number(field.value):field.value.trim();
      if(field.dataset.h2Format==="text")value=field.value;
      if(field.dataset.h2Format==="json")value=JSON.parse(value);
      if(field.dataset.h2Format==="lines")value=value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean);
      set(body.settings,field.dataset.h2Path,value);
    }
    body.settings.blocked_countries=[...form.querySelector("[data-h2-countries]").selectedOptions].map(v=>v.value);
    body.endpoint_host=form.elements.public_domain.value.trim();body.endpoint_port=Number(form.elements.hysteria_port.value);
    delete body.applied_config_version_ms;delete body.config_error_id;delete body.obfs_password_set;
    delete body.file_set;
    body.file_set_id=form.hysteriaFileSet?form.hysteriaFileSet.id:initial.file_set_id||"";
    return body;
  }
  function bind(form,initial,upload) {
    bindFiles(form,initial,upload);
    bindString(form);
    const update=()=>{
      for(const block of form.querySelectorAll("[data-h2-when]")){
        const [path,expected]=block.dataset.h2When.split("="),field=form.querySelector(`[data-h2-path="${path}"]`),value=field?.type==="checkbox"?String(field.checked):field?.value;
        block.hidden=expected.startsWith("!")?value===expected.slice(1):value!==expected;
        for(const input of block.querySelectorAll("input,select,textarea")) input.disabled=block.hidden;
      }
      const warning=form.querySelector("[data-h2-warning]");if(warning)warning.hidden=false;
    };
    form.addEventListener("change",update);update();
    form.querySelector("[data-h2-password]").addEventListener("click",()=>{
      const bytes=crypto.getRandomValues(new Uint8Array(24));form.querySelector('[data-h2-path="obfs.password"]').value=Array.from(bytes,b=>b.toString(16).padStart(2,"0")).join("");
    });
    const rerenderOutbounds=body=>{
      initial.settings.outbounds=body.settings.outbounds;
      form.querySelector("[data-h2-outbounds]").innerHTML=body.settings.outbounds.map((o,i)=>outbound(body.settings,o,i)).join("");update();
    };
    form.querySelector("[data-h2-add-outbound]").addEventListener("click",()=>{try{const body=read(form,initial);body.settings.outbounds.push({name:"exit-"+body.settings.outbounds.length,type:"direct",mode:"auto",bindIPv4:"",bindIPv6:"",bindDevice:"",fastOpen:false,addr:"",username:"",password:"",url:"",insecure:false});rerenderOutbounds(body)}catch(e){error(form,e.message)}});
    form.addEventListener("click",e=>{const b=e.target.closest("[data-h2-remove-outbound]");if(!b)return;try{const body=read(form,initial);if(body.settings.outbounds.length===1)throw new Error("Нужен хотя бы один выход");body.settings.outbounds.splice(Number(b.dataset.h2RemoveOutbound),1);rerenderOutbounds(body)}catch(e){error(form,e.message)}});
  }
  function bindFiles(form,initial,upload) {
    form.hysteriaFileSet=clone(initial.file_set||{id:initial.file_set_id||"",files:[],size:0});
    const panel=form.querySelector(".h2-file-upload"),list=panel.querySelector("[data-h2-file-list]"),status=panel.querySelector("[data-h2-file-status]");
    const format=size=>size>=1048576?(size/1048576).toFixed(2)+" МиБ":size>=1024?(size/1024).toFixed(1)+" КиБ":size+" Б";
    const render=()=>{const set=form.hysteriaFileSet;list.innerHTML=set.files.length?`<p>Файлы: ${set.files.length} · ${format(set.size)}</p><ul class="h2-files-list">${set.files.map(file=>`<li><span>${escape(file.path)}</span><span>${format(file.size)}</span><button type="button" data-h2-remove-file="${escape(file.path)}" title="Удалить файл" aria-label="Удалить ${escape(file.path)}">×</button></li>`).join("")}</ul>`:'<p class="helper">Файлы не загружены</p>'};render();
    let controller;
    form.closest("dialog")?.addEventListener("close",()=>controller?.abort(),{once:true});
    const request=async body=>{
      if(form.dataset.h2Uploading==="true")return;
      if(typeof upload!=="function"){status.textContent="Загрузка файлов недоступна";status.hidden=false;return}
      controller=new AbortController();form.dataset.h2Uploading="true";
      panel.querySelectorAll("button").forEach(b=>b.disabled=true);status.hidden=false;status.textContent="Загрузка…";
      try{const result=await upload(body,controller.signal);if(!form.isConnected)return;form.hysteriaFileSet=result;form.querySelector('[data-h2-path="masquerade.dir"]').value=result.id?managedFilesRoot+"/"+result.id:"";render();status.textContent="Черновик обновлён. Нажмите «Сохранить и применить»."}
      catch(e){if(e.name!=="AbortError")status.textContent=e.message||"Не удалось загрузить файлы"}
      finally{form.dataset.h2Uploading="false";panel.querySelectorAll("button").forEach(b=>b.disabled=false)}
    };
    for(const kind of ["files","zip"]){
      const picker=panel.querySelector(`[data-h2-pick-${kind}]`);
      panel.querySelector(`[data-h2-upload-${kind}]`).addEventListener("click",()=>picker.click());
      picker.addEventListener("change",async()=>{
        const files=[...picker.files];if(!files.length)return;
        const total=files.reduce((sum,f)=>sum+f.size,0);
        if(files.length>128||total>8388608||files.some(f=>f.size>(kind==="zip"?2097152:4194304))){status.hidden=false;status.textContent="Превышен лимит количества или размера файлов";picker.value="";return}
        const data=new FormData();for(const file of files)data.append(kind==="zip"?"archive":"files",file);
        await request(data);picker.value="";
      });
    }
    panel.addEventListener("click",event=>{const b=event.target.closest("[data-h2-remove-file]");if(b)void request({source_id:form.hysteriaFileSet.id,remove_paths:[b.dataset.h2RemoveFile]})});
  }
  function error(form,message){const target=form.querySelector("[data-h2-result]");target.textContent=message;target.hidden=false;target.scrollIntoView({block:"nearest"})}
  function duration(value) {
    const units={ns:1e-9,us:1e-6,ms:1e-3,s:1,m:60,h:3600},parts=[...String(value).matchAll(/(\d+(?:\.\d+)?)(ns|us|ms|s|m|h)/g)];
    return parts.map(p=>p[0]).join("")===value&&parts.length?parts.reduce((sum,p)=>sum+Number(p[1])*units[p[2]],0):NaN;
  }
  function validateSettings(c,hasPassword=false) {
    const fail=message=>{throw new Error(message)};
    if(c.obfs.type!=="off"&&!hasPassword&&new TextEncoder().encode(c.obfs.password).length<4)fail("Пароль обфускации должен содержать минимум 4 байта");
    if(c.obfs.password&&new TextEncoder().encode(c.obfs.password).length<4)fail("Пароль обфускации должен содержать минимум 4 байта");
    if(c.obfs.type==="gecko"&&(!Number.isInteger(c.obfs.minPacketSize)||!Number.isInteger(c.obfs.maxPacketSize)||c.obfs.minPacketSize<1||c.obfs.maxPacketSize<c.obfs.minPacketSize||c.obfs.maxPacketSize>2048))fail("Размеры Gecko: 1 ≤ минимальный ≤ максимальный ≤ 2048");
    for(const [a,b] of [["initStreamReceiveWindow","maxStreamReceiveWindow"],["initConnReceiveWindow","maxConnReceiveWindow"]])if(!Number.isSafeInteger(c.quic[a])||!Number.isSafeInteger(c.quic[b])||c.quic[a]<16384||c.quic[b]<c.quic[a])fail("Некорректные окна QUIC");
    if(!Number.isSafeInteger(c.quic.maxIncomingStreams)||c.quic.maxIncomingStreams<8)fail("Максимум потоков: целое число не меньше 8");
    const idle=duration(c.quic.maxIdleTimeout);if(!(idle>=4&&idle<=120))fail("Таймаут QUIC: от 4s до 120s");
    for(const t of [c.udpIdleTimeout,c.resolver.timeout,c.sniff.timeout])if(!(duration(t)>0))fail("Таймаут должен быть положительным, например 30s");
    for(const speed of [c.bandwidth.up,c.bandwidth.down])if(!/^(0|\d+\s*(bps|b|kbps|kb|k|mbps|mb|m|gbps|gb|g|tbps|tb|t))$/i.test(speed))fail("Скорость: целое число с единицей, например 200 mbps, или 0");
    const hostPort=value=>{const match=value.match(/^(?:\[[0-9a-f:]+\]|[^:/\s]+)?:(\d+)$/i);return match&&Number(match[1])>=1&&Number(match[1])<=65535};
    if(c.resolver.type!=="system"&&!hostPort(c.resolver.addr))fail("Укажите DNS в формате адрес:порт");
    for(const ports of [c.sniff.tcpPorts,c.sniff.udpPorts])if(ports!=="all"){for(const range of ports.split(",")){const p=range.trim().split("-").map(Number);if(p.length>2||p.some(n=>!Number.isInteger(n)||n<1||n>65535)||(p.length===2&&p[1]<p[0]))fail("Порты: all, 443 или 8000-9000")}}
    const httpURL=value=>{try{return ["http:","https:"].includes(new URL(value).protocol)}catch(_){return false}};
    if(c.masquerade.type==="proxy"&&!httpURL(c.masquerade.url))fail("Укажите HTTP/HTTPS URL маскировки");
    if(c.masquerade.type==="file"&&!c.masquerade.dir.startsWith("/"))fail("Загрузите файлы или ZIP для маскировки");
    if(c.masquerade.type==="string"&&(!Number.isInteger(c.masquerade.statusCode)||c.masquerade.statusCode<200||c.masquerade.statusCode>599))fail("HTTP-код: 200–599");
    for(const addr of [c.masquerade.listenHTTP,c.masquerade.listenHTTPS])if(addr&&!hostPort(addr))fail("Адрес маскировки: IP:порт или :порт");
    if(c.masquerade.type!=="off"&&c.masquerade.listenHTTP&&!c.masquerade.listenHTTPS)fail("Для HTTP-маскировки также нужен HTTPS адрес:порт");
    if(c.masquerade.type!=="off"&&c.masquerade.forceHTTPS&&(!c.masquerade.listenHTTP||!c.masquerade.listenHTTPS))fail("Для перенаправления нужны HTTP и HTTPS адреса");
    if(!c.masquerade.headers||Array.isArray(c.masquerade.headers)||typeof c.masquerade.headers!=="object")fail("HTTP-заголовки должны быть JSON-объектом");
    for(const [key,value] of Object.entries(c.masquerade.headers))if(typeof value!=="string"||!key||/[\r\n]/.test(key+value))fail("Некорректные HTTP-заголовки");
    const names=new Set();if(!c.outbounds.length)fail("Нужен хотя бы один выход");
    for(const o of c.outbounds){if(!o.name||names.has(o.name))fail("Имена выходов должны быть непустыми и уникальными");names.add(o.name);if(o.type==="direct"&&o.bindDevice&&(o.bindIPv4||o.bindIPv6))fail("Выберите IP или интерфейс привязки");if(o.type==="socks5"&&!hostPort(o.addr))fail("SOCKS5: укажите адрес:порт");if(o.type==="http"&&!httpURL(o.url))fail("Укажите HTTP/HTTPS URL прокси")}
    return true;
  }
  function validate(form,initial) {
    if(form.dataset.h2Uploading==="true"){error(form,"Дождитесь завершения загрузки файлов");return false}
    for(const f of form.querySelectorAll("[data-h2-path]"))f.setCustomValidity("");
    try{
      const body=read(form,initial),c=body.settings;
      if(c.masquerade.type==="file"&&(form.hysteriaFileSet?.id||!c.masquerade.dir)&&!form.hysteriaFileSet?.files.some(file=>file.path==="index.html"))throw new Error("В корне набора должен быть файл index.html (/index.html). Файл во вложенной папке не подходит.");
      validateSettings(c,initial.obfs_password_set);
      if(!Array.isArray(c.acl_rules)||!c.masquerade.headers||Array.isArray(c.masquerade.headers)||typeof c.masquerade.headers!=="object")throw new Error("Некорректные заголовки или ACL");
    }catch(e){error(form,e.message);return false}
    for(const f of form.querySelectorAll(":invalid")){const section=f.closest("details");if(section)section.open=true}
    return form.reportValidity();
  }
  function open(initial,name,save,load,upload) {
    const dialog=window.lazUI.openModal({id:"h2-settings-dialog",title:`Настройки Hysteria · ${name}`,wide:true,className:"awg-settings-dialog",body:`<form id="h2-settings-form" novalidate class="laz-modal-fields">${renderFields(initial)}</form>`,initialFocus:'[name="public_domain"]',actions:'<button type="button" data-close-dialog>Закрыть</button><button type="submit" form="h2-settings-form" data-h2-save>Сохранить и применить</button>'});
    const form=dialog.querySelector("form");bind(form,initial,upload);let pending=false;
    form.addEventListener("submit",async e=>{e.preventDefault();if(pending||!validate(form,initial))return;pending=true;dialog.querySelector("[data-h2-save]").disabled=true;error(form,"Сохранение…");
      try{const submitted=read(form,initial);const saved=await save(submitted);if(form.hysteriaFileSet?.id===submitted.file_set_id)form.querySelector('[data-h2-file-status]').hidden=true;initial.config_version_ms=saved.config_version_ms;error(form,"Настройки сохранены. Ожидаем применения…");let latest=saved;
        for(let i=0;i<120&&dialog.isConnected;i++){await new Promise(resolve=>setTimeout(resolve,1000));latest=await load();if(latest.config_error_id)throw new Error("Не удалось применить настройки. Код ошибки: "+latest.config_error_id);if(latest.applied_config_version_ms>=saved.config_version_ms){error(form,"Настройки сохранены и применены");initial=latest;return}}
        error(form,"Настройки сохранены. Применение продолжится после восстановления связи с сервером.");
      }catch(e){error(form,e.message)}finally{pending=false;if(dialog.isConnected)dialog.querySelector("[data-h2-save]").disabled=false}
    });return dialog;
  }
  window.LazHysteriaSettings={defaults,renderFields,read,bind,validate,validateSettings,open};
})();
