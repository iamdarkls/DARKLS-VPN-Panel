'use strict';
// The controller owns saved-server cards, both dialogs and every credential draft.
class SavedServersController {
  constructor() {
    const owner = this;
    this.postConnection = this.postConnection.bind(this);
    this.authorizeConnection = this.authorizeConnection.bind(this);
    this.secretArea = this.secretArea.bind(this);
    this.syncEditorMode = this.syncEditorMode.bind(this);
    this.syncTransportFields = this.syncTransportFields.bind(this);
    this.closeConnectionEditor = this.closeConnectionEditor.bind(this);
    this.openConnectionEditor = this.openConnectionEditor.bind(this);
    this.populateConnection = this.populateConnection.bind(this);
    this.connectionPayload = this.connectionPayload.bind(this);
    this.connectionRequest = this.connectionRequest.bind(this);
    this.clearConnectionSecrets = this.clearConnectionSecrets.bind(this);
    this.loadConnections = this.loadConnections.bind(this);

    // Server cards contain metadata only. Credentials are revealed into a modal,
    // never into storage, and cleared on cancel, save and logout.
    this.serverView = $('servers-main');
    this.connectionBox = box(this.serverView, 'Текущее подключение', 'Сейчас роутер использует это подключение. Сохранение копии не меняет работающий VPN.');
    this.connectionBox.classList.add('current-connection');
    this.connectionFacts = output(this.connectionBox);
    button(this.connectionBox, '+ Добавить сервер', ()=>this.openConnectionEditor(), 'primary');
    button(this.connectionBox, 'Сохранить текущее в список', async ()=>{
      const done=await this.authorizeConnection('Сохранить текущее подключение', 'Создадим проверенную копию текущего сервера. Работающий VPN не изменится.', 'Сохранить копию', {name:'Текущее подключение'},
        data=>this.postConnection('connection-snapshot',data));
      if (done) { await this.loadConnections(); notice('Текущее подключение сохранено в списке.') }
    });
    this.previousConnectionButton = button(this.connectionBox, 'Вернуть предыдущее подключение', async ()=>{
      cleanDraft();
      const done=await this.authorizeConnection('Вернуть предыдущее подключение', 'Проверим прежний сервер и переключим VPN. Открытые VPN-соединения переподключатся.', 'Проверить и вернуть', {},data=>this.postConnection('connection-rollback',data));
      if (done) { await this.loadConnections(); await status(); notice('Предыдущее подключение восстановлено.') }
    });
    this.bankBox=box(this.serverView,'Мои серверы','Добавь сервер, сохрани его после проверки и подключайся отдельной кнопкой. Переименование и удаление копии не меняют работающий VPN.');
    this.bankList=el('div','server-list'); this.bankList.id='server-list'; this.bankBox.append(this.bankList);
    this.bankItems=[], this.connectionBase=null, this.editingConnectionID='';

    this.authDialog=el('dialog','server-auth-dialog'); this.authDialog.setAttribute('aria-labelledby','server-auth-title');
    this.authForm=el('form'), this.authTitle=el('h2'); this.authTitle.id='server-auth-title';
    this.authDescription=el('p','muted');
    this.authProgress=el('p','operation-progress');this.authProgress.setAttribute('role','status');
    this.authForm.append(this.authTitle,this.authDescription);
    this.authName=field(this.authForm,'Новое название сервера'); this.authName.maxLength=60;
    this.authPassword=field(this.authForm,'Текущий пароль панели','password'); this.authPassword.required=true;
    this.authActions=el('div','dialog-actions'),this.authCancel=el('button','secondary','Отмена'),this.authSubmit=el('button','primary','Продолжить');
    this.authCancel.type='button';this.authSubmit.type='submit';this.authActions.append(this.authCancel,this.authSubmit);this.authForm.append(this.authProgress,this.authActions);this.authDialog.append(this.authForm);document.body.append(this.authDialog);
    this.authPending=false;
    this.authCancel.onclick=()=>this.authDialog.close();
    this.authDialog.addEventListener('cancel',event=>{if(this.authPending)event.preventDefault()});

    this.connectionDialog=el('dialog','connection-dialog');this.connectionDialog.setAttribute('aria-labelledby','connection-editor-title');
    this.editorHeading=el('div','dialog-heading'),this.editorTitle=el('h2');this.editorTitle.id='connection-editor-title';
    this.editorClose=el('button','icon-button','×');this.editorClose.type='button';this.editorClose.setAttribute('aria-label','Закрыть редактор сервера');this.editorHeading.append(this.editorTitle,this.editorClose);this.connectionDialog.append(this.editorHeading);
    this.editorHelp=el('p','muted');this.connectionDialog.append(this.editorHelp);
    this.bankName=field(this.connectionDialog,'Название сервера');this.bankName.maxLength=60;
    this.editorMode=select(this.connectionDialog,[['link','Ссылка VLESS или outbound JSON'],['manual','Параметры вручную']],'Способ ввода');

    this.rawWrap=el('div');this.connectionDialog.append(this.rawWrap);
    this.connectionRaw=this.secretArea(this.rawWrap,'VLESS-ссылка или outbound JSON');
    this.importButton=button(this.rawWrap,'Разобрать в ручные поля',async()=>{
      const d=await this.connectionRequest('connection-parse',{outbound:this.connectionRaw.value});this.populateConnection(d.outbound);this.editorMode.value=this.connectionRawMode.checked?'link':'manual';this.syncEditorMode();notice('Параметры разобраны. Сервер ещё не сохранён.');
    });
    this.connectionRawMode={get checked(){return owner.editorMode.value==='link'},set checked(value){owner.editorMode.value=value?'link':'manual'}};
    this.connectionFields=el('div','connection-fields hidden');this.connectionDialog.append(this.connectionFields);
    this.connectionGrid=el('div','settings-grid');this.connectionFields.append(this.connectionGrid);
    this.conn={};
    for(const [key,label,type] of [
     ['address','Адрес сервера','text'],['port','Порт сервера','number'],['id','UUID VLESS','text'],['encryption','Encryption','text'],['flow','Flow (можно оставить пустым)','text'],['sni','SNI / serverName','text'],['fp','TLS fingerprint','text'],['pbk','Reality publicKey','text'],['sid','Reality shortId','text'],['spx','Reality spiderX','text'],['path','Путь транспорта','text'],['host','Host транспорта','text'],['serviceName','gRPC serviceName','text'],['authority','gRPC authority (необязательно)','text'],['alpn','TLS ALPN (через запятую)','text']
    ]){this.conn[key]=field(this.connectionGrid,label,type);this.conn[key].dataset.secret='true';this.conn[key].autocomplete='off'}
    this.conn.port.min=1;this.conn.port.max=65535;
    this.conn.network=select(this.connectionGrid,['tcp','raw','xhttp','ws','grpc'].map(x=>[x,x]),'Транспорт');
    this.conn.security=select(this.connectionGrid,['none','reality','tls'].map(x=>[x,x]),'Защита транспорта');
    this.conn.mode=select(this.connectionGrid,['auto','packet-up','stream-up','stream-one'].map(x=>[x,x]),'Режим XHTTP');
    this.conn.grpcMode=select(this.connectionGrid,[['gun','Gun — обычный'],['multi','Multi — многопоточный']],'Режим gRPC');
    this.grpcHint=el('p','tiny muted','serviceName и режим должны совпадать с сервером. Authority задаётся отдельно от SNI; для gRPC через TLS нужен HTTP/2 (h2).');this.connectionFields.append(this.grpcHint);
    this.conn.extra=this.secretArea(this.connectionFields,'XHTTP extra JSON (сохраняет XMUX и дополнительные настройки)');
    this.editorProgress=el('p','operation-progress');this.editorProgress.setAttribute('role','status');this.connectionDialog.append(this.editorProgress);
    this.connectionPassword=field(this.connectionDialog,'Пароль для управления подключениями','password');
    this.connectionDialog.append(el('p','tiny muted','Пароль нужен для чтения и изменения реквизитов. Они остаются на роутере, не сохраняются в браузере и очищаются при закрытии окна.'));
    this.editorActions=el('div','editor-actions');this.connectionDialog.append(this.editorActions);
    this.editorTest=button(this.editorActions,'Проверить соединение',async()=>{await this.connectionRequest('connection-test',{outbound:this.connectionPayload()});notice('Изолированная HTTPS-проверка пройдена.')});
    this.editorSave=button(this.editorActions,'Проверить и сохранить сервер',async()=>{
      if(!this.bankName.value.trim())throw new Error('Укажи название сервера.');
      await this.connectionRequest('connection-save',{id:this.editingConnectionID,name:this.bankName.value.trim(),outbound:this.connectionPayload()});
      this.closeConnectionEditor();await this.loadConnections();notice('Сервер сохранён. Для переключения нажми «Подключить» на его карточке.');
    },'primary');
    this.advancedApply=el('details','advanced-apply');this.advancedApply.append(el('summary','','Применить без сохранения в список'));this.connectionDialog.append(this.advancedApply);
    button(this.advancedApply,'Применить как основное',async()=>{
     cleanDraft();if(!confirm('Проверить и заменить основной сервер без сохранения копии в список? Соединения переподключатся.'))return;
     await this.connectionRequest('connection-apply',{outbound:this.connectionPayload()});this.closeConnectionEditor();await this.loadConnections();await status();notice('Основное подключение изменено и проверено.');
    });
    document.body.append(this.connectionDialog);
    this.editorPending=false;

    this.editorMode.onchange=this.syncEditorMode;

    this.conn.network.onchange=this.syncTransportFields;this.conn.security.onchange=this.syncTransportFields;

    this.editorClose.onclick=this.closeConnectionEditor;
    this.connectionDialog.addEventListener('cancel',event=>{if(this.editorPending)event.preventDefault();else this.clearConnectionSecrets()});
    this.connectionDialog.addEventListener('close',()=>{if(!this.connectionDialog.open)this.clearConnectionSecrets()});


    this.showLoginBeforeConnections=showLogin;
    showLogin=()=>{
     if(this.connectionDialog.open)this.connectionDialog.close();if(this.authDialog.open)this.authDialog.close();
     this.clearConnectionSecrets();clearToasts();activityRequest=null;activityAt=0;activityShown.clear();this.showLoginBeforeConnections();
    };
  }
  async postConnection(path,data){
    const session=csrf,result=await api(path,'POST',data);
    if (!session || csrf!==session) throw new Error('Сеанс изменился; войди заново.');
    return result;
  }

  authorizeConnection(title,description,label,options,perform){
    if(this.authDialog.open || this.authForm.onsubmit) throw new Error('Сначала заверши открытое действие.');
    this.authTitle.textContent=title;this.authDescription.textContent=description;this.authSubmit.textContent=label;
    this.authName.parentElement.classList.toggle('hidden',options.name===undefined);
    this.authName.value=options.name || '';this.authPassword.value='';this.authName.required=options.name!==undefined;
    let result=null;
    const requestSession=csrf;
    return new Promise(resolve=>{
      this.authDialog.addEventListener('close',()=>{this.authPassword.value='';this.authName.value='';this.authForm.onsubmit=null;resolve(result)},{once:true});
      this.authForm.onsubmit=event=>{
        event.preventDefault();if(this.authPending)return;
        this.authPending=true;this.authCancel.disabled=true;this.authProgress.textContent='Выполняем действие… Дождись завершения.';
        action(this.authSubmit,async()=>{
          try {
            const value=await perform({password:this.authPassword.value,...(options.name!==undefined?{name:this.authName.value.trim()}:{})});
            if(!csrf || csrf!==requestSession)return;
            result=value || true;this.authDialog.close();
          } finally {this.authPassword.value='';this.authPending=false;this.authCancel.disabled=false;this.authProgress.textContent=''}
        });
      };
      this.authDialog.showModal();
    });
  }

  secretArea(parent,label){
    const wrap=el('label','',label),n=el('textarea');n.rows=4;n.spellcheck=false;n.dataset.secret='true';n.autocomplete='off';wrap.append(n);parent.append(wrap);return n;
  }

  syncEditorMode(){this.syncTransportFields();this.rawWrap.classList.toggle('hidden',this.editorMode.value!=='link');this.connectionFields.classList.toggle('hidden',this.editorMode.value!=='manual')}

  syncTransportFields(){
   const network=this.conn.network.value,security=this.conn.security.value;
   for(const key of ['sni','fp'])this.conn[key].parentElement.classList.toggle('hidden',security==='none');
   for(const key of ['pbk','sid','spx'])this.conn[key].parentElement.classList.toggle('hidden',security!=='reality');
   for(const key of ['path','host'])this.conn[key].parentElement.classList.toggle('hidden',!['xhttp','ws'].includes(network));
   for(const key of ['serviceName','authority','grpcMode'])this.conn[key].parentElement.classList.toggle('hidden',network!=='grpc');
   this.grpcHint.classList.toggle('hidden',network!=='grpc');
   this.conn.alpn.parentElement.classList.toggle('hidden',security!=='tls');
   for(const key of ['mode','extra'])this.conn[key].parentElement.classList.toggle('hidden',network!=='xhttp');
  }

  closeConnectionEditor(){this.clearConnectionSecrets();this.connectionDialog.close();notificationCenter.restoreRegion()}

  openConnectionEditor(item=null,out=null){
   this.clearConnectionSecrets();this.editingConnectionID=item?.id || '';this.bankName.value=item?.name || '';
   this.editorTitle.textContent=item?'Изменить сервер':'Новый сервер';
   this.editorHelp.textContent=item?'Сохраняем изменения в этой копии. Работающий VPN изменится только после нажатия «Подключить» на карточке.':'Вставь ссылку и задай название. Проверим соединение перед сохранением. Подключение выполняется отдельно.';
   this.editorMode.value='link';if(out)this.populateConnection(out);this.syncEditorMode();this.connectionDialog.showModal();
  }

  populateConnection(out){
    this.connectionBase = structuredClone(out);
    this.connectionRaw.value = JSON.stringify(out, null, 2);
    const st = out.streamSettings || {}, server = out.settings?.vnext?.[0] || {}, user = server.users?.[0] || {};
    const tls = (st.security === 'reality' ? st.realitySettings : st.tlsSettings) || {}, transport = (st.network === 'ws' ? st.wsSettings : st.xhttpSettings) || {};
    this.connectionRawMode.checked = out.protocol !== 'vless' || !['tcp','raw','xhttp','ws','grpc'].includes(st.network || 'tcp');
    for (const [k, v] of Object.entries({address:server.address, port:server.port, id:user.id,
      encryption:user.encryption || 'none', flow:user.flow, sni:tls.serverName, fp:tls.fingerprint,
      pbk:tls.publicKey, sid:tls.shortId, spx:tls.spiderX, path:transport.path,
      host:transport.host || transport.headers?.Host, serviceName:st.grpcSettings?.serviceName,
      authority:st.grpcSettings?.authority, grpcMode:st.grpcSettings?.multiMode === true ? 'multi' : 'gun', alpn:(Array.isArray(st.tlsSettings?.alpn) ? st.tlsSettings.alpn : []).join(','),
      network:st.network || 'tcp', security:st.security || 'none', mode:st.xhttpSettings?.mode || 'auto',
      extra:JSON.stringify(st.xhttpSettings?.extra || {}, null, 2)})) this.conn[k].value = v ?? '';
    this.syncTransportFields();
  }

  // Preserve unfamiliar transport fields while editing known VLESS fields.
  connectionPayload(){
    if (this.connectionRawMode.checked) {
      if (!this.connectionRaw.value.trim()) throw new Error('Вставь ссылку или outbound JSON.');
      return this.connectionRaw.value.trim();
    }
    const out = this.connectionBase ? structuredClone(this.connectionBase) : {protocol:'vless',settings:{vnext:[{users:[{}]}]}};
    if (out.protocol !== 'vless') throw new Error('Для этого протокола используй outbound JSON.');
    if (!this.conn.address.value.trim() || !this.conn.id.value.trim() || !Number.isInteger(Number(this.conn.port.value)) || Number(this.conn.port.value)<1 || Number(this.conn.port.value)>65535) throw new Error('Укажи адрес, порт 1–65535 и UUID.');
    out.tag = 'vpn'; out.settings ||= {}; out.settings.vnext ||= [{}];
    const server = out.settings.vnext[0] ||= {}; server.address = this.conn.address.value.trim(); server.port = Number(this.conn.port.value); server.users ||= [{}]; server.users[0] ||= {};
    Object.assign(server.users[0], {id:this.conn.id.value.trim(),encryption:this.conn.encryption.value.trim() || 'none'});
    if (this.conn.flow.value.trim()) server.users[0].flow = this.conn.flow.value.trim(); else delete server.users[0].flow;
    const st = out.streamSettings ||= {}; st.network = this.conn.network.value; st.security = this.conn.security.value;
    if (st.security !== 'none') {
      const key = st.security === 'reality' ? 'realitySettings' : 'tlsSettings'; const tls = st[key] ||= {};
      tls.serverName = this.conn.sni.value.trim(); tls.fingerprint = this.conn.fp.value.trim();
      if (st.security === 'tls') { const alpn=this.conn.alpn.value.split(',').map(x=>x.trim()).filter(Boolean);if(alpn.length)tls.alpn=alpn;else delete tls.alpn }
      if (st.security === 'reality') Object.assign(tls,{publicKey:this.conn.pbk.value.trim(),shortId:this.conn.sid.value.trim(),spiderX:this.conn.spx.value.trim()});
    }
    if (st.network === 'xhttp') {
      let extra;
      try { extra = JSON.parse(this.conn.extra.value || '{}') } catch { throw new Error('XHTTP extra должен быть JSON-объектом.') }
      if (!extra || Array.isArray(extra) || typeof extra !== 'object') throw new Error('XHTTP extra должен быть JSON-объектом.');
      Object.assign(st.xhttpSettings ||= {}, {path:this.conn.path.value, host:this.conn.host.value, mode:this.conn.mode.value, extra});
    }
    if (st.network === 'ws') { const ws = st.wsSettings ||= {}; ws.path = this.conn.path.value; (ws.headers ||= {}).Host = this.conn.host.value; }
    if (st.network === 'grpc') Object.assign(st.grpcSettings ||= {}, {serviceName:this.conn.serviceName.value,authority:this.conn.authority.value,multiMode:this.conn.grpcMode.value==='multi'});
    return JSON.stringify(out);
  }

  async connectionRequest(path,data={}){
    if(this.editorPending)throw new Error('Проверка сервера ещё выполняется.');
    if(!this.connectionPassword.value)throw new Error('Введи пароль панели для проверки или сохранения сервера.');
    this.editorPending=true;this.editorClose.disabled=true;this.editorProgress.textContent=path==='connection-parse'?'Разбираем параметры…':'Проверяем соединение… Дождись завершения.';
    const controls=[...this.connectionDialog.querySelectorAll('input,textarea,select,button')];
    const states=controls.map(n=>n.disabled);controls.forEach(n=>n.disabled=true);
    try{return await this.postConnection(path,{password:this.connectionPassword.value,...data})}
    finally {this.connectionPassword.value='';this.editorPending=false;this.editorProgress.textContent='';controls.forEach((n,i)=>n.disabled=states[i]);this.editorClose.disabled=false}
  }

  clearConnectionSecrets(){
   this.connectionBase=null;this.editingConnectionID='';this.connectionRaw.value='';this.connectionPassword.value='';this.bankName.value='';
   Object.values(this.conn).forEach(n=>{if(n.dataset.secret)n.value=''});
  }

  async loadConnections(){
   const session=csrf,d=await api('connections');if(!session || session!==csrf)return;
   this.bankItems=d.items || [];this.bankList.replaceChildren();
   this.connectionBox.querySelector('p.muted').textContent=(d.profile?.active==='backup'?'Выбран резервный профиль. ':'Выбран основной профиль. ')+'Сохранение копии не меняет работающий VPN.';
   facts(this.connectionFacts,[['Протокол',d.current?.protocol || 'Не определён'],['Транспорт',d.current?.transport || 'tcp'],['Защита',d.current?.security || 'none'],['Сохранено серверов',this.bankItems.length+' / '+(d.limit || 20)]]);
   this.previousConnectionButton.disabled=!d.rollback_available;this.previousConnectionButton.dataset.locked=String(this.previousConnectionButton.disabled);
   if(!this.bankItems.length)this.bankList.append(el('div','empty','Здесь будут твои серверы. Добавь новый по ссылке или сохрани текущее подключение.'));
   for(const item of this.bankItems){
    const card=el('article','server-card'+(item.active?' is-active':''));card.dataset.serverId=item.id;
    const head=el('div','server-card-head'),name=el('h3','',item.name),badge=el('span','route-tag',item.active?'Подключён':item.primary?'Основной профиль':'Сохранён');head.append(name,badge);
    card.append(head,el('p','muted server-meta',[item.protocol || 'VPN',item.transport || 'tcp',item.security || 'none'].join(' · ')),el('p','tiny muted','Сохранён: '+stamp(item.updated_at)));
    const actions=el('div','server-actions');card.append(actions);
    const connect=button(actions,item.active?'Уже подключён':'Подключить',async()=>{
      cleanDraft();const done=await this.authorizeConnection('Подключить «'+item.name+'»','Проверим сервер и переключим основной VPN. Соединения переподключатся; при ошибке вернём прежний канал.','Проверить и подключить',{},data=>this.postConnection('connection-use',{...data,id:item.id}));
      if(done){await this.loadConnections();await status();notice('Сервер «'+item.name+'» подключён.')}
    },item.active?'secondary':'primary');connect.disabled=!!item.active;connect.dataset.locked=String(connect.disabled);
    button(actions,'Изменить',async()=>{
      const data=await this.authorizeConnection('Изменить «'+item.name+'»','Пароль нужен, чтобы открыть реквизиты этого сервера.','Открыть параметры',{},data=>this.postConnection('connection-reveal',{...data,id:item.id}));
      if(data)this.openConnectionEditor(item,data.outbound);
    });
    button(actions,'Переименовать',async()=>{
      const done=await this.authorizeConnection('Переименовать сервер','Меняется только название в списке. VPN не переподключится.','Сохранить название',{name:item.name},data=>this.postConnection('connection-rename',{...data,id:item.id}));
      if(done){await this.loadConnections();notice('Название сервера сохранено.')}
    });
    button(actions,'Удалить',async()=>{
      const done=await this.authorizeConnection('Удалить «'+item.name+'»?','Удалится только копия из списка. Работающее подключение останется в конфигурации роутера.','Удалить копию',{},data=>this.postConnection('connection-delete',{...data,id:item.id}));
      if(done){await this.loadConnections();notice('Сохранённая копия удалена. Рабочее подключение не изменено.')}
    },'danger');this.bankList.append(card);
   }
  }

}
const serverController = new SavedServersController();
function loadConnections() { return serverController.loadConnections() }
function openConnectionEditor(item, outbound) { return serverController.openConnectionEditor(item, outbound) }

async function loadDiagnosticDevices() {
  const session = csrf, d = await api('devices'); if (!session || session !== csrf) return;
  const n = $('diagnose-device'), selected = n.value;
  n.replaceChildren();
  for (const item of [{mac:'',name:'Это устройство'}, ...(d.devices || []).filter(x=>x.ip)]) {
    const o = el('option','',item.name + (item.ip ? ' · ' + item.ip : '')); o.value = item.mac; n.append(o);
  }
  n.value = [...n.options].some(o=>o.value===selected) ? selected : '';
}
const dailyBox = box($('stability-history'), 'Сводка за последние 24 часа', 'Доступность по периодическим HTTPS-пробам общего туннеля. Состояние отдельных сайтов может отличаться.');
$('stability-history').prepend(dailyBox);
const dailyFacts = output(dailyBox), dailyNote = el('p','tiny muted'); dailyBox.append(dailyNote);
const activityBox = box($('stability-history'), 'Уведомления о связи', 'Последние события за сутки. Отметка прочитанного сохраняется только в этом браузере.');
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
