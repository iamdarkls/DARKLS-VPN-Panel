'use strict';
// Credentials exist only in this form's memory. Read-only polling never reveals
// them or replaces a draft; revealing/importing requires password + CSRF.
const connectionBox = box($('settings-view').querySelector('.settings-grid'), 'Основное VPN-подключение',
  'Импортируй VLESS-ссылку или outbound JSON. Перед заменой проверяем отдельное соединение и полный конфиг; при ошибке возвращаем прежнее подключение. Списки сайтов и устройств сохраняются.');
connectionBox.classList.add('wide');
const connectionFacts = output(connectionBox);
const connectionPassword = field(connectionBox, 'Пароль для управления подключениями', 'password');
function secretArea(parent, label) {
  const wrap = el('label', '', label), n = el('textarea');
  n.rows = 4; n.spellcheck = false; n.dataset.secret = 'true'; n.autocomplete = 'off';
  wrap.append(n); parent.append(wrap); return n;
}
const connectionRaw = secretArea(connectionBox, 'VLESS-ссылка или outbound JSON');
const connectionRawMode = check(connectionBox, 'Использовать JSON / ссылку напрямую вместо полей ниже');
const connectionFields = el('details', 'connection-fields');
connectionFields.append(el('summary', '', 'Параметры VLESS вручную'));
connectionBox.append(connectionFields);
const connectionGrid = el('div', 'settings-grid'); connectionFields.append(connectionGrid);
const conn = {};
for (const [key, label, type] of [
  ['address','Адрес сервера','text'], ['port','Порт сервера','number'], ['id','UUID VLESS','text'],
  ['encryption','Encryption','text'], ['flow','Flow (можно оставить пустым)','text'],
  ['sni','SNI / serverName','text'], ['fp','TLS fingerprint','text'],
  ['pbk','Reality publicKey','text'], ['sid','Reality shortId','text'], ['spx','Reality spiderX','text'],
  ['path','Путь транспорта','text'], ['host','Host транспорта','text'], ['serviceName','gRPC serviceName','text']
]) {
  conn[key] = field(connectionGrid, label, type); conn[key].dataset.secret = 'true'; conn[key].autocomplete = 'off';
}
conn.port.min = 1; conn.port.max = 65535;
conn.network = select(connectionGrid, ['tcp','raw','xhttp','ws','grpc'].map(x=>[x,x]), 'Транспорт');
conn.security = select(connectionGrid, ['none','reality','tls'].map(x=>[x,x]), 'Защита транспорта');
conn.mode = select(connectionGrid, ['auto','packet-up','stream-up','stream-one'].map(x=>[x,x]), 'Режим XHTTP');
conn.extra = secretArea(connectionFields, 'XHTTP extra JSON (сохраняет XMUX и дополнительные настройки)');
let connectionBase = null;
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
    network:st.network || 'tcp', security:st.security || 'none', mode:st.xhttpSettings?.mode || 'auto',
    extra:JSON.stringify(st.xhttpSettings?.extra || {}, null, 2)})) conn[k].value = v ?? '';
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
    if (st.security === 'reality') Object.assign(tls,{publicKey:conn.pbk.value.trim(),shortId:conn.sid.value.trim(),spiderX:conn.spx.value.trim()});
  }
  if (st.network === 'xhttp') {
    let extra;
    try { extra = JSON.parse(conn.extra.value || '{}') } catch { throw new Error('XHTTP extra должен быть JSON-объектом.') }
    if (!extra || Array.isArray(extra) || typeof extra !== 'object') throw new Error('XHTTP extra должен быть JSON-объектом.');
    Object.assign(st.xhttpSettings ||= {}, {path:conn.path.value, host:conn.host.value, mode:conn.mode.value, extra});
  }
  if (st.network === 'ws') { const ws = st.wsSettings ||= {}; ws.path = conn.path.value; (ws.headers ||= {}).Host = conn.host.value; }
  if (st.network === 'grpc') (st.grpcSettings ||= {}).serviceName = conn.serviceName.value;
  return JSON.stringify(out);
}
async function connectionRequest(path, data = {}) {
  const session = csrf;
  try {
    const result = await api(path, 'POST', {password:connectionPassword.value, ...data});
    if (!session || csrf !== session) throw new Error('Сеанс изменился; войди заново.');
    return result;
  } finally { connectionPassword.value = '' }
}
button(connectionBox, 'Импортировать ссылку / JSON в поля', async () => {
  const d = await connectionRequest('connection-parse', {outbound:connectionRaw.value});
  populateConnection(d.outbound); notice('Параметры импортированы. Рабочее подключение ещё не изменено.');
});
button(connectionBox, 'Загрузить параметры текущего подключения', async () => {
  const d = await connectionRequest('connection-reveal', {id:'current'}); populateConnection(d.outbound);
});
button(connectionBox, 'Проверить новое подключение', async () => {
  await connectionRequest('connection-test', {outbound:connectionPayload()}); notice('Изолированная HTTPS-проверка пройдена.');
});
button(connectionBox, 'Применить как основное', async () => {
  cleanDraft();
  if (!confirm('Проверить и заменить основной VPN-сервер? Текущие VPN-соединения переподключатся; при ошибке выполним откат.')) return;
  await connectionRequest('connection-apply', {outbound:connectionPayload()});
  clearConnectionSecrets(); notice('Основное подключение изменено и проверено.'); await loadConnections(); await status();
}, 'primary');
button(connectionBox, 'Скрыть параметры подключения', clearConnectionSecrets);
const previousConnectionButton = button(connectionBox, 'Вернуть предыдущее подключение', async () => {
  cleanDraft(); if (!confirm('Проверить и вернуть предыдущее подключение? VPN-соединения переподключатся.')) return;
  await connectionRequest('connection-rollback'); clearConnectionSecrets(); await loadConnections(); await status(); notice('Предыдущее подключение восстановлено.');
});
connectionBox.append(el('p','tiny muted','Ключи сохраняются на роутере в файлах с правами 0600. В браузере они доступны после ввода пароля; очищаются при выходе. Панель работает по HTTP — используй её только в доверенной домашней сети.'));
const bankBox = box($('settings-view').querySelector('.settings-grid'), 'Сохранённые серверы',
  'До 20 именованных подключений. Сохранение проверяет соединение и создаёт копию; активный сервер меняется отдельной кнопкой. Удаление копии не выключает работающий VPN.');
bankBox.classList.add('wide');
const bankSelect = select(bankBox, [['','Новый сервер']], 'Сохранённое подключение');
const bankName = field(bankBox, 'Название сервера'); bankName.maxLength = 60;
button(bankBox, 'Загрузить выбранный сервер в поля', async () => {
  if (!bankSelect.value) throw new Error('Выбери сохранённый сервер.');
  populateConnection((await connectionRequest('connection-reveal',{id:bankSelect.value})).outbound);
});
button(bankBox, 'Сохранить параметры из полей', async () => {
  if (bankSelect.value && !confirm('Заменить сохранённую копию? Рабочее подключение не изменится.')) return;
  await connectionRequest('connection-save',{id:bankSelect.value,name:bankName.value,outbound:connectionPayload()});
  notice('Проверенная копия подключения сохранена.'); await loadConnections();
});
button(bankBox, 'Переключиться на выбранный сервер', async () => {
  cleanDraft(); if (!bankSelect.value) throw new Error('Выбери сохранённый сервер.');
  if (!confirm('Проверить выбранный сервер и сделать его основным? VPN-соединения переподключатся.')) return;
  await connectionRequest('connection-use',{id:bankSelect.value}); clearConnectionSecrets(); await loadConnections(); await status(); notice('Сервер переключён.');
},'primary');
button(bankBox, 'Удалить сохранённую копию', async () => {
  if (!bankSelect.value) throw new Error('Выбери сохранённый сервер.');
  if (!confirm('Удалить только сохранённую копию? Работающий сервер останется в конфигурации.')) return;
  await connectionRequest('connection-delete',{id:bankSelect.value}); bankSelect.value = ''; bankName.value = ''; await loadConnections();
},'danger');
let bankItems = [];
bankSelect.onchange = () => { bankName.value = bankItems.find(x=>x.id===bankSelect.value)?.name || '' };
async function loadConnections() {
  const session = csrf, d = await api('connections'); if (!session || session !== csrf) return;
  const selected = bankSelect.value; bankItems = d.items || [];
  bankSelect.replaceChildren();
  for (const item of [{id:'',name:'Новый сервер'}, ...bankItems]) {
    const o = el('option','',item.name + (item.active ? ' · активен' : item.primary ? ' · основной' : '')); o.value = item.id; bankSelect.append(o);
  }
  bankSelect.value = bankItems.some(x=>x.id===selected) ? selected : '';
  previousConnectionButton.disabled = !d.rollback_available; previousConnectionButton.dataset.locked = String(previousConnectionButton.disabled);
  facts(connectionFacts,[['Протокол',d.current?.protocol || 'Не определён'],['Транспорт',d.current?.transport || 'tcp'],['Защита',d.current?.security || 'none'],['Сохранено серверов',bankItems.length + ' / ' + (d.limit || 20)]]);
}
function clearConnectionSecrets() {
  connectionBase = null; connectionRaw.value = ''; connectionPassword.value = '';
  Object.values(conn).forEach(n=>{ if (n.dataset.secret) n.value = '' });
}
const showLoginBeforeConnections = showLogin;
showLogin = function() { clearConnectionSecrets(); activityRequest = null; activityAt = 0; activityBanner.classList.add('hidden'); showLoginBeforeConnections() };
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
const activityBanner = el('div','notice hidden'); activityBanner.setAttribute('role','status');
const activityBannerText = el('span'); activityBanner.append(activityBannerText);
$('notice').before(activityBanner);
let activityLatest = '', activityRead = '';
try { activityRead = localStorage.getItem('darkls-notifications-read') || '' } catch {}
button(activityBanner,'Прочитано',()=>{
  activityRead = activityLatest; try { localStorage.setItem('darkls-notifications-read',activityRead) } catch {}
  activityBanner.classList.add('hidden');
});
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
    activityBannerText.textContent = newest ? stamp(newest.at)+' · '+newest.message+' ' : '';
    activityBanner.classList.toggle('hidden',!newest || activityLatest === activityRead);
  })();
  activityRequest = pending.finally(()=>{ if (activityRequest === tracked) activityRequest = null });
  const tracked = activityRequest; return tracked;
}
// Reuse the existing visible-tab status cadence rather than adding a timer.
const statusBeforeActivity = status;
status = async function() { const result = await statusBeforeActivity(); if (csrf) await loadActivity().catch(()=>{}); return result };
