'use strict';
// Registry callbacks resolve feature modules when a page is opened, after startup.
const views = {
  sites: {title:'Сайты и сервисы', subtitle:'Выбирай, что открывать через твой VPN.'},
  devices: {title:'Устройства', subtitle:'Определи, какие устройства используют выбранные маршруты.', load:()=>loadDevices(), poll:true},
  servers: {title:'VPN-серверы', subtitle:'Подключения, сохранённые серверы и безопасное переключение.', tabs:{
    'servers-main':()=>loadConnections(), 'servers-backup':()=>loadProfiles()
  }},
  diagnostics: {title:'Диагностика', subtitle:'Соединение, DNS и маршрутизация сайтов.', load:()=>loadDiagnosticDevices()},
  stability: {title:'Контроль связи', subtitle:'История доступности, восстановление и автоматический контроль.', tabs:{
    'stability-history':async()=>{await loadActivity(true);await loadServiceHistory()},
    'stability-monitor':()=>loadMonitorSettings()
  }},
  tuning: {title:'Настройка связи', subtitle:'QUIC, параметры XHTTP и задержка под нагрузкой.'},
  updates: {title:'Обновления', subtitle:'Панель с GitHub, ядро Xray и ручная установка — в одном месте.', tabs:{
    'updates-panel':()=>loadGitHub(), 'updates-core':()=>loadCore()
  }},
  backups: {title:'Копии и файлы', subtitle:'Восстановление настроек, импорт, экспорт и очистка места.', tabs:{
    'backups-history':()=>loadBackups(), 'backups-files':()=>loadStorage()
  }},
  metrics: {title:'Ресурсы роутера', subtitle:'Фактическая память, CPU и свободное место.', load:()=>loadMetrics(), poll:true},
  logs: {title:'Журнал событий', subtitle:'Что менялось и почему восстанавливался VPN.', load:()=>loadLogs(), poll:true},
  settings: {title:'Пароль панели', subtitle:'Управление доступом к локальной панели.'}
};
let viewBusy = false;

// Keep navigation in memory: it never changes router settings or discards form drafts.
function selectedTab(group) {
  return document.querySelector(`[data-tabs="${group}"] [aria-selected="true"]`)?.dataset.tab;
}
let refreshAgain = false;
async function refreshView() {
  // If the user navigates during a slow read, refresh the latest destination next.
  if (viewBusy) { refreshAgain = true; return }
  viewBusy = true;
  const view = currentView, tab = selectedTab(view);
  try {
    const destination = views[view];
    const load = destination?.tabs ? destination.tabs[tab] : destination?.load;
    if (load) await load();
  } catch (e) {
    notice(e.message, true)
  } finally {
    viewBusy = false;
    if (refreshAgain) {
      refreshAgain = false;
      if (csrf) await refreshView()
    }
  }
}
function openView(view) {
  if (!views[view]) return;
  currentView = view;
  document.querySelectorAll('.nav').forEach(b => {
    const active = b.dataset.view === view;
    b.classList.toggle('active', active);
    if (active) b.setAttribute('aria-current', 'page');
    else b.removeAttribute('aria-current')
  });
  for (const key of Object.keys(views)) $(key + '-view').classList.toggle('hidden', view !== key);
  $('page-title').textContent = views[view].title;
  $('page-subtitle').textContent = views[view].subtitle;
  return refreshView()
}
function initNavigation() {
  document.querySelectorAll('.nav').forEach(button => button.onclick = () => openView(button.dataset.view));
  // WAI-ARIA tabs: one tab stop, arrow navigation, Home/End and labelled panels.
  document.querySelectorAll('[data-tabs]').forEach(list => {
    const buttons = [...list.querySelectorAll('[role="tab"]')];
    function selectTab(button) {
      for (const other of buttons) {
        const active = other === button;
        other.setAttribute('aria-selected', String(active));
        other.tabIndex = active ? 0 : -1;
        $(other.dataset.tab).classList.toggle('hidden', !active)
      }
      if (currentView === list.dataset.tabs) refreshView()
    }
    for (const [index, button] of buttons.entries()) {
      button.onclick = () => selectTab(button);
      button.onkeydown = event => {
        const target = {ArrowRight:(index+1)%buttons.length, ArrowLeft:(index+buttons.length-1)%buttons.length, Home:0, End:buttons.length-1}[event.key];
        if (target === undefined) return;
        event.preventDefault();
        buttons[target].focus();
        selectTab(buttons[target])
      }
    }
  });
}
