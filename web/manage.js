'use strict';
// Server cards contain metadata only. Credentials are revealed into a modal,
// never into storage, and cleared on cancel, save and logout.
const serverView = $('servers-view');
const connectionBox = box(serverView, 'Текущее подключение', 'Сейчас роутер использует это подключение. Сохранение копии не меняет работающий VPN.');
connectionBox.classList.add('current-connection');
const connectionFacts = output(connectionBox);
button(connectionBox, '+ Добавить сервер', ()=>openConnectionEditor(), 'primary');
button(connectionBox, 'Сохранить текущее в список', async ()=>{
  const done=await authorizeConnection('Сохранить текущее подключение', 'Создадим проверенную копию текущего сервера. Работающий VPN не изменится.', 'Сохранить копию', {name:'Текущее подключение'},
    data=>postConnection('connection-snapshot',data));
  if (done) { await loadConnections(); notice('Текущее подключение сохранено в списке.') }
});
const previousConnectionButton = button(connectionBox, 'Вернуть предыдущее подключение', async ()=>{
  cleanDraft();
  const done=await authorizeConnection('Вернуть предыдущее подключение', 'Проверим прежний сервер и переключим VPN. Открытые VPN-соединения переподключатся.', 'Проверить и вернуть', {},data=>postConnection('connection-rollback',data));
  if (done) { await loadConnections(); await status(); notice('Предыдущее подключение восстановлено.') }
});
const bankBox=box(serverView,'Мои серверы','Добавь сервер, сохрани его после проверки и подключайся отдельной кнопкой. Переименование и удаление копии не меняют работающий VPN.');
const bankList=el('div','server-list'); bankList.id='server-list'; bankBox.append(bankList);
let bankItems=[], connectionBase=null, editingConnectionID='';

const authDialog=el('dialog','server-auth-dialog'); authDialog.setAttribute('aria-labelledby','server-auth-title');
const authForm=el('form'), authTitle=el('h2'); authTitle.id='server-auth-title';
const authDescription=el('p','muted');
const authProgress=el('p','operation-progress');authProgress.setAttribute('role','status');
authForm.append(authTitle,authDescription);
const authName=field(authForm,'Новое название сервера'); authName.maxLength=60;
const authPassword=field(authForm,'Текущий пароль панели','password'); authPassword.required=true;
const authActions=el('div','dialog-actions'),authCancel=el('button','secondary','Отмена'),authSubmit=el('button','primary','Продолжить');
authCancel.type='button';authSubmit.type='submit';authActions.append(authCancel,authSubmit);authForm.append(authProgress,authActions);authDialog.append(authForm);document.body.append(authDialog);
let authPending=false;
authCancel.onclick=()=>authDialog.close();
authDialog.addEventListener('cancel',event=>{if(authPending)event.preventDefault()});
async function postConnection(path,data) {
  const session=csrf,result=await api(path,'POST',data);
  if (!session || csrf!==session) throw new Error('Сеанс изменился; войди заново.');
  return result;
}
function authorizeConnection(title,description,label,options,perform) {
  if(authDialog.open || authForm.onsubmit) throw new Error('Сначала заверши открытое действие.');
  authTitle.textContent=title;authDescription.textContent=description;authSubmit.textContent=label;
  authName.parentElement.classList.toggle('hidden',options.name===undefined);
  authName.value=options.name || '';authPassword.value='';authName.required=options.name!==undefined;
  let result=null;
  const requestSession=csrf;
  return new Promise(resolve=>{
    authDialog.addEventListener('close',()=>{authPassword.value='';authName.value='';authForm.onsubmit=null;resolve(result)},{once:true});
    authForm.onsubmit=event=>{
      event.preventDefault();if(authPending)return;
      authPending=true;authCancel.disabled=true;authProgress.textContent='Выполняем действие… Дождись завершения.';
      action(authSubmit,async()=>{
        try {
          const value=await perform({password:authPassword.value,...(options.name!==undefined?{name:authName.value.trim()}:{})});
          if(!csrf || csrf!==requestSession)return;
          result=value || true;authDialog.close();
        } finally {authPassword.value='';authPending=false;authCancel.disabled=false;authProgress.textContent=''}
      });
    };
    authDialog.showModal();
  });
}

const connectionDialog=el('dialog','connection-dialog');connectionDialog.setAttribute('aria-labelledby','connection-editor-title');
const editorHeading=el('div','dialog-heading'),editorTitle=el('h2');editorTitle.id='connection-editor-title';
const editorClose=el('button','icon-button','×');editorClose.type='button';editorClose.setAttribute('aria-label','Закрыть редактор сервера');editorHeading.append(editorTitle,editorClose);connectionDialog.append(editorHeading);
const editorHelp=el('p','muted');connectionDialog.append(editorHelp);
const bankName=field(connectionDialog,'Название сервера');bankName.maxLength=60;
const editorMode=select(connectionDialog,[['link','Ссылка VLESS или outbound JSON'],['manual','Параметры вручную']],'Способ ввода');
function secretArea(parent,label) {
  const wrap=el('label','',label),n=el('textarea');n.rows=4;n.spellcheck=false;n.dataset.secret='true';n.autocomplete='off';wrap.append(n);parent.append(wrap);return n;
}
const rawWrap=el('div');connectionDialog.append(rawWrap);
const connectionRaw=secretArea(rawWrap,'VLESS-ссылка или outbound JSON');
const importButton=button(rawWrap,'Разобрать в ручные поля',async()=>{
  const d=await connectionRequest('connection-parse',{outbound:connectionRaw.value});populateConnection(d.outbound);editorMode.value=connectionRawMode.checked?'link':'manual';syncEditorMode();notice('Параметры разобраны. Сервер ещё не сохранён.');
});
const connectionRawMode={get checked(){return editorMode.value==='link'},set checked(value){editorMode.value=value?'link':'manual'}};
const connectionFields=el('div','connection-fields hidden');connectionDialog.append(connectionFields);
const connectionGrid=el('div','settings-grid');connectionFields.append(connectionGrid);
const conn={};
for(const [key,label,type] of [
 ['address','Адрес сервера','text'],['port','Порт сервера','number'],['id','UUID VLESS','text'],['encryption','Encryption','text'],['flow','Flow (можно оставить пустым)','text'],['sni','SNI / serverName','text'],['fp','TLS fingerprint','text'],['pbk','Reality publicKey','text'],['sid','Reality shortId','text'],['spx','Reality spiderX','text'],['path','Путь транспорта','text'],['host','Host транспорта','text'],['serviceName','gRPC serviceName','text'],['authority','gRPC authority (необязательно)','text'],['alpn','TLS ALPN (через запятую)','text']
]){conn[key]=field(connectionGrid,label,type);conn[key].dataset.secret='true';conn[key].autocomplete='off'}
conn.port.min=1;conn.port.max=65535;
conn.network=select(connectionGrid,['tcp','raw','xhttp','ws','grpc'].map(x=>[x,x]),'Транспорт');
conn.security=select(connectionGrid,['none','reality','tls'].map(x=>[x,x]),'Защита транспорта');
conn.mode=select(connectionGrid,['auto','packet-up','stream-up','stream-one'].map(x=>[x,x]),'Режим XHTTP');
conn.grpcMode=select(connectionGrid,[['gun','Gun — обычный'],['multi','Multi — многопоточный']],'Режим gRPC');
const grpcHint=el('p','tiny muted','serviceName и режим должны совпадать с сервером. Authority задаётся отдельно от SNI; для gRPC через TLS нужен HTTP/2 (h2).');connectionFields.append(grpcHint);
conn.extra=secretArea(connectionFields,'XHTTP extra JSON (сохраняет XMUX и дополнительные настройки)');
const editorProgress=el('p','operation-progress');editorProgress.setAttribute('role','status');connectionDialog.append(editorProgress);
const connectionPassword=field(connectionDialog,'Пароль для управления подключениями','password');
connectionDialog.append(el('p','tiny muted','Пароль нужен для чтения и изменения реквизитов. Они остаются на роутере, не сохраняются в браузере и очищаются при закрытии окна.'));
const editorActions=el('div','editor-actions');connectionDialog.append(editorActions);
const editorTest=button(editorActions,'Проверить соединение',async()=>{await connectionRequest('connection-test',{outbound:connectionPayload()});notice('Изолированная HTTPS-проверка пройдена.')});
const editorSave=button(editorActions,'Проверить и сохранить сервер',async()=>{
  if(!bankName.value.trim())throw new Error('Укажи название сервера.');
  await connectionRequest('connection-save',{id:editingConnectionID,name:bankName.value.trim(),outbound:connectionPayload()});
  closeConnectionEditor();await loadConnections();notice('Сервер сохранён. Для переключения нажми «Подключить» на его карточке.');
},'primary');
const advancedApply=el('details','advanced-apply');advancedApply.append(el('summary','','Применить без сохранения в список'));connectionDialog.append(advancedApply);
button(advancedApply,'Применить как основное',async()=>{
 cleanDraft();if(!confirm('Проверить и заменить основной сервер без сохранения копии в список? Соединения переподключатся.'))return;
 await connectionRequest('connection-apply',{outbound:connectionPayload()});closeConnectionEditor();await loadConnections();await status();notice('Основное подключение изменено и проверено.');
});
document.body.append(connectionDialog);
let editorPending=false;
function syncEditorMode(){syncTransportFields();rawWrap.classList.toggle('hidden',editorMode.value!=='link');connectionFields.classList.toggle('hidden',editorMode.value!=='manual')}
editorMode.onchange=syncEditorMode;
function syncTransportFields(){
 const network=conn.network.value,security=conn.security.value;
 for(const key of ['sni','fp'])conn[key].parentElement.classList.toggle('hidden',security==='none');
 for(const key of ['pbk','sid','spx'])conn[key].parentElement.classList.toggle('hidden',security!=='reality');
 for(const key of ['path','host'])conn[key].parentElement.classList.toggle('hidden',!['xhttp','ws'].includes(network));
 for(const key of ['serviceName','authority','grpcMode'])conn[key].parentElement.classList.toggle('hidden',network!=='grpc');
 grpcHint.classList.toggle('hidden',network!=='grpc');
 conn.alpn.parentElement.classList.toggle('hidden',security!=='tls');
 for(const key of ['mode','extra'])conn[key].parentElement.classList.toggle('hidden',network!=='xhttp');
}
conn.network.onchange=syncTransportFields;conn.security.onchange=syncTransportFields;

function closeConnectionEditor(){clearConnectionSecrets();connectionDialog.close();document.body.append(toastRegion)}
editorClose.onclick=closeConnectionEditor;
connectionDialog.addEventListener('cancel',event=>{if(editorPending)event.preventDefault();else clearConnectionSecrets()});
connectionDialog.addEventListener('close',()=>{if(!connectionDialog.open)clearConnectionSecrets()});
function openConnectionEditor(item=null,out=null){
 clearConnectionSecrets();editingConnectionID=item?.id || '';bankName.value=item?.name || '';
 editorTitle.textContent=item?'Изменить сервер':'Новый сервер';
 editorHelp.textContent=item?'Сохраняем изменения в этой копии. Работающий VPN изменится только после нажатия «Подключить» на карточке.':'Вставь ссылку и задай название. Проверим соединение перед сохранением. Подключение выполняется отдельно.';
 editorMode.value='link';if(out)populateConnection(out);syncEditorMode();connectionDialog.showModal();
}
function populateConnection(out) {
  connectionBase = structuredClone(out);
  connectionRaw.value = JSON.stringify(out, null, 2);
  const st = out.streamSettings || {}, server = out.settings?.vnext?.[0] || {}, user = server.users?.[0] || {};
  const tls = (st.security === 'reality' ? st.realitySettings : st.tlsSettings) || {}, transport = (st.network === 'ws' ? st.wsSettings : st.xhttpSettings) || {};
  connectionRawMode.checked = out.protocol !== 'vless' || !['tcp','raw','xhttp','ws','grpc'].includes(st.network || 'tcp');
  for (const [k, v] of Object.entries({address:server.address, port:server.port, id:user.id,
    encryption:user.encryption || 'none', flow:user.flow, sni:tls.serverName, fp:tls.fingerprint,
    pbk:tls.publicKey, sid:tls.shortId, spx:tls.spiderX, path:transport.path,
    host:transport.host || transport.headers?.Host, serviceName:st.grpcSettings?.serviceName,
    authority:st.grpcSettings?.authority, grpcMode:st.grpcSettings?.multiMode === true ? 'multi' : 'gun', alpn:(Array.isArray(st.tlsSettings?.alpn) ? st.tlsSettings.alpn : []).join(','),
    network:st.network || 'tcp', security:st.security || 'none', mode:st.xhttpSettings?.mode || 'auto',
    extra:JSON.stringify(st.xhttpSettings?.extra || {}, null, 2)})) conn[k].value = v ?? '';
  syncTransportFields();
}
// Clone the imported outbound so unfamiliar fields (including TLS/transport
// options) survive editing. Advanced JSON remains available for other protocols.
function connectionPayload() {
  if (connectionRawMode.checked) {
    if (!connectionRaw.value.trim()) throw new Error('Вставь ссылку или outbound JSON.');
    return connectionRaw.value.trim();
  }
  const out = connectionBase ? structuredClone(connectionBase) : {protocol:'vless',settings:{vnext:[{users:[{}]}]}};
  if (out.protocol !== 'vless') throw new Error('Для этого протокола используй outbound JSON.');
  if (!conn.address.value.trim() || !conn.id.value.trim() || !Number.isInteger(Number(conn.port.value)) || Number(conn.port.value)<1 || Number(conn.port.value)>65535) throw new Error('Укажи адрес, порт 1–65535 и UUID.');
  out.tag = 'vpn'; out.settings ||= {}; out.settings.vnext ||= [{}];
  const server = out.settings.vnext[0] ||= {}; server.address = conn.address.value.trim(); server.port = Number(conn.port.value); server.users ||= [{}]; server.users[0] ||= {};
  Object.assign(server.users[0], {id:conn.id.value.trim(),encryption:conn.encryption.value.trim() || 'none'});
  if (conn.flow.value.trim()) server.users[0].flow = conn.flow.value.trim(); else delete server.users[0].flow;
  const st = out.streamSettings ||= {}; st.network = conn.network.value; st.security = conn.security.value;
  if (st.security !== 'none') {
    const key = st.security === 'reality' ? 'realitySettings' : 'tlsSettings'; const tls = st[key] ||= {};
    tls.serverName = conn.sni.value.trim(); tls.fingerprint = conn.fp.value.trim();
    if (st.security === 'tls') { const alpn=conn.alpn.value.split(',').map(x=>x.trim()).filter(Boolean);if(alpn.length)tls.alpn=alpn;else delete tls.alpn }
    if (st.security === 'reality') Object.assign(tls,{publicKey:conn.pbk.value.trim(),shortId:conn.sid.value.trim(),spiderX:conn.spx.value.trim()});
  }
  if (st.network === 'xhttp') {
    let extra;
    try { extra = JSON.parse(conn.extra.value || '{}') } catch { throw new Error('XHTTP extra должен быть JSON-объектом.') }
    if (!extra || Array.isArray(extra) || typeof extra !== 'object') throw new Error('XHTTP extra должен быть JSON-объектом.');
    Object.assign(st.xhttpSettings ||= {}, {path:conn.path.value, host:conn.host.value, mode:conn.mode.value, extra});
  }
  if (st.network === 'ws') { const ws = st.wsSettings ||= {}; ws.path = conn.path.value; (ws.headers ||= {}).Host = conn.host.value; }
  if (st.network === 'grpc') Object.assign(st.grpcSettings ||= {}, {serviceName:conn.serviceName.value,authority:conn.authority.value,multiMode:conn.grpcMode.value==='multi'});
  return JSON.stringify(out);
}

async function connectionRequest(path,data={}) {
  if(editorPending)throw new Error('Проверка сервера ещё выполняется.');
  if(!connectionPassword.value)throw new Error('Введи пароль панели для проверки или сохранения сервера.');
  editorPending=true;editorClose.disabled=true;editorProgress.textContent=path==='connection-parse'?'Разбираем параметры…':'Проверяем соединение… Дождись завершения.';
  const controls=[...connectionDialog.querySelectorAll('input,textarea,select,button')];
  const states=controls.map(n=>n.disabled);controls.forEach(n=>n.disabled=true);
  try{return await postConnection(path,{password:connectionPassword.value,...data})}
  finally {connectionPassword.value='';editorPending=false;editorProgress.textContent='';controls.forEach((n,i)=>n.disabled=states[i]);editorClose.disabled=false}
}
function clearConnectionSecrets(){
 connectionBase=null;editingConnectionID='';connectionRaw.value='';connectionPassword.value='';bankName.value='';
 Object.values(conn).forEach(n=>{if(n.dataset.secret)n.value=''});
}
async function loadConnections(){
 const session=csrf,d=await api('connections');if(!session || session!==csrf)return;
 bankItems=d.items || [];bankList.replaceChildren();
 connectionBox.querySelector('p.muted').textContent=(d.profile?.active==='backup'?'Выбран резервный профиль. ':'Выбран основной профиль. ')+'Сохранение копии не меняет работающий VPN.';
 facts(connectionFacts,[['Протокол',d.current?.protocol || 'Не определён'],['Транспорт',d.current?.transport || 'tcp'],['Защита',d.current?.security || 'none'],['Сохранено серверов',bankItems.length+' / '+(d.limit || 20)]]);
 previousConnectionButton.disabled=!d.rollback_available;previousConnectionButton.dataset.locked=String(previousConnectionButton.disabled);
 if(!bankItems.length)bankList.append(el('div','empty','Здесь будут твои серверы. Добавь новый по ссылке или сохрани текущее подключение.'));
 for(const item of bankItems){
  const card=el('article','server-card'+(item.active?' is-active':''));card.dataset.serverId=item.id;
  const head=el('div','server-card-head'),name=el('h3','',item.name),badge=el('span','route-tag',item.active?'Подключён':item.primary?'Основной профиль':'Сохранён');head.append(name,badge);
  card.append(head,el('p','muted server-meta',[item.protocol || 'VPN',item.transport || 'tcp',item.security || 'none'].join(' · ')),el('p','tiny muted','Сохранён: '+stamp(item.updated_at)));
  const actions=el('div','server-actions');card.append(actions);
  const connect=button(actions,item.active?'Уже подключён':'Подключить',async()=>{
    cleanDraft();const done=await authorizeConnection('Подключить «'+item.name+'»','Проверим сервер и переключим основной VPN. Соединения переподключатся; при ошибке вернём прежний канал.','Проверить и подключить',{},data=>postConnection('connection-use',{...data,id:item.id}));
    if(done){await loadConnections();await status();notice('Сервер «'+item.name+'» подключён.')}
  },item.active?'secondary':'primary');connect.disabled=!!item.active;connect.dataset.locked=String(connect.disabled);
  button(actions,'Изменить',async()=>{
    const data=await authorizeConnection('Изменить «'+item.name+'»','Пароль нужен, чтобы открыть реквизиты этого сервера.','Открыть параметры',{},data=>postConnection('connection-reveal',{...data,id:item.id}));
    if(data)openConnectionEditor(item,data.outbound);
  });
  button(actions,'Переименовать',async()=>{
    const done=await authorizeConnection('Переименовать сервер','Меняется только название в списке. VPN не переподключится.','Сохранить название',{name:item.name},data=>postConnection('connection-rename',{...data,id:item.id}));
    if(done){await loadConnections();notice('Название сервера сохранено.')}
  });
  button(actions,'Удалить',async()=>{
    const done=await authorizeConnection('Удалить «'+item.name+'»?','Удалится только копия из списка. Работающее подключение останется в конфигурации роутера.','Удалить копию',{},data=>postConnection('connection-delete',{...data,id:item.id}));
    if(done){await loadConnections();notice('Сохранённая копия удалена. Рабочее подключение не изменено.')}
  },'danger');bankList.append(card);
 }
}
const showLoginBeforeConnections=showLogin;
showLogin=function(){
 if(connectionDialog.open)connectionDialog.close();if(authDialog.open)authDialog.close();
 clearConnectionSecrets();clearToasts();activityRequest=null;activityAt=0;activityShown.clear();showLoginBeforeConnections();
};
async function loadDiagnosticDevices() {
  const session = csrf, d = await api('devices'); if (!session || session !== csrf) return;
  const n = $('diagnose-device'), selected = n.value;
  n.replaceChildren();
  for (const item of [{mac:'',name:'Это устройство'}, ...(d.devices || []).filter(x=>x.ip)]) {
    const o = el('option','',item.name + (item.ip ? ' · ' + item.ip : '')); o.value = item.mac; n.append(o);
  }
  n.value = [...n.options].some(o=>o.value===selected) ? selected : '';
}
const dailyBox = box($('stability-view'), 'Сводка за последние 24 часа', 'Доступность по периодическим HTTPS-пробам общего туннеля. Состояние отдельных сайтов может отличаться.');
const dailyFacts = output(dailyBox), dailyNote = el('p','tiny muted'); dailyBox.append(dailyNote);
const activityBox = box($('stability-view'), 'Уведомления о связи', 'Последние события за сутки. Отметка прочитанного сохраняется только в этом браузере.');
const activityList = output(activityBox);
let activityLatest = '', activityRead = '';
const activityShown = new Set();
try { activityRead = localStorage.getItem('darkls-notifications-read') || '' } catch {}
function showActivityToast(item) {
  if (!item || item.id === activityRead || activityShown.has(item.id)) return;
  activityShown.add(item.id);
  if (activityShown.size > 100) activityShown.delete(activityShown.values().next().value);
  toastMessage(item.message, {id:'vpn-'+item.id, title:'Состояние VPN', error:item.kind === 'vpn_error' || item.kind === 'update_error', persistent:true, onClose:()=>{
    activityRead=item.id;
    try { localStorage.setItem('darkls-notifications-read',item.id) } catch {}
  }});
}
let activityAt = 0, activityRequest = null;
function duration(seconds) { const n = Math.max(0,Number(seconds)||0); return Math.floor(n/3600)+' ч '+Math.floor(n%3600/60)+' мин '+Math.floor(n%60)+' с' }
function loadActivity(force = false) {
  if (!csrf || document.hidden || (!force && Date.now()-activityAt < 60000)) return Promise.resolve();
  if (activityRequest) return activityRequest;
  const session = csrf;
  const pending = (async()=>{
    const d = await api('activity'); if (!session || session !== csrf) return;
    activityAt = Date.now(); const s = d.summary || {};
    facts(dailyFacts,[['Время наблюдения',duration(s.observed_seconds)],['Недоступность в наблюдаемые интервалы',duration(s.unavailable_seconds)],['Нет данных',duration(s.unknown_seconds)],['Эпизоды, начавшиеся за сутки',s.failure_episodes || 0],['Попытки восстановления',s.recoveries || 0],['Последняя проба',!s.probe_fresh ? 'Нет свежей пробы' : s.probe_ok ? 'Успешна' : 'Ошибка'],['Пик занятой RAM',s.resource_samples ? (s.peak_used_kib/1024).toFixed(1)+' МиБ' : 'Нет замеров'],['Пик RSS панели / Xray',s.resource_samples ? (s.peak_panel_kib/1024).toFixed(1)+' / '+(s.peak_xray_kib/1024).toFixed(1)+' МиБ' : 'Нет замеров']]);
    dailyNote.textContent = s.note || '';
    activityList.replaceChildren();
    for (const item of d.notifications || []) activityList.append(el('p','',stamp(item.at)+' · '+item.message));
    if (!d.notifications?.length) activityList.append(el('p','muted','Событий за сутки нет.'));
    const newest = d.notifications?.[0]; activityLatest = newest?.id || '';
    showActivityToast(newest);
  })();
  activityRequest = pending.finally(()=>{ if (activityRequest === tracked) activityRequest = null });
  const tracked = activityRequest; return tracked;
}
// Reuse the existing visible-tab status cadence rather than adding a timer.
const statusBeforeActivity = status;
status = async function() { const result = await statusBeforeActivity(); if (csrf) await loadActivity().catch(()=>{}); return result };
