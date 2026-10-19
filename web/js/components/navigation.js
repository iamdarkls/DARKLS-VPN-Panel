'use strict';
// Loaders are resolved when a page is opened, after feature modules initialize.
const panelViews = {
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

class PanelNavigation {
  constructor({views, session, reportError}) {
    this.views = views;
    this.session = session;
    this.reportError = reportError;
    this.current = 'sites';
    this.busy = false;
    this.refreshAgain = false;
  }

  selectedTab(group = this.current) {
    return document.querySelector(`[data-tabs="${group}"] [aria-selected="true"]`)?.dataset.tab;
  }

  shouldPoll() { return !!this.views[this.current]?.poll }

  async refresh() {
    // Coalesce navigation during slow I/O into one read of the latest page.
    if (this.busy) { this.refreshAgain = true; return }
    this.busy = true;
    const destination = this.views[this.current], tab = this.selectedTab();
    try {
      const load = destination?.tabs ? destination.tabs[tab] : destination?.load;
      if (load) await load();
    } catch (error) {
      this.reportError(error.message);
    } finally {
      this.busy = false;
      if (this.refreshAgain) {
        this.refreshAgain = false;
        if (this.session()) await this.refresh();
      }
    }
  }

  open(view) {
    if (!this.views[view]) return;
    this.current = view;
    document.querySelectorAll('.nav').forEach(button => {
      const active = button.dataset.view === view;
      button.classList.toggle('active', active);
      if (active) button.setAttribute('aria-current', 'page');
      else button.removeAttribute('aria-current');
    });
    for (const key of Object.keys(this.views)) $(key + '-view').classList.toggle('hidden', view !== key);
    $('page-title').textContent = this.views[view].title;
    $('page-subtitle').textContent = this.views[view].subtitle;
    return this.refresh();
  }

  init() {
    document.querySelectorAll('.nav').forEach(button => {
      button.onclick = () => this.open(button.dataset.view);
    });
    // One tab stop; arrow keys and Home/End follow WAI-ARIA tabs behaviour.
    document.querySelectorAll('[data-tabs]').forEach(list => {
      const buttons = [...list.querySelectorAll('[role="tab"]')];
      const select = button => {
        for (const other of buttons) {
          const active = other === button;
          other.setAttribute('aria-selected', String(active));
          other.tabIndex = active ? 0 : -1;
          $(other.dataset.tab).classList.toggle('hidden', !active);
        }
        if (this.current === list.dataset.tabs) this.refresh();
      };
      for (const [index, button] of buttons.entries()) {
        button.onclick = () => select(button);
        button.onkeydown = event => {
          const target = {ArrowRight:(index+1)%buttons.length, ArrowLeft:(index+buttons.length-1)%buttons.length, Home:0, End:buttons.length-1}[event.key];
          if (target === undefined) return;
          event.preventDefault();
          buttons[target].focus();
          select(buttons[target]);
        };
      }
    });
  }
}

const panelNavigation = new PanelNavigation({
  views: panelViews,
  session: () => csrf,
  reportError: message => notice(message, true)
});
// Small integration functions let existing feature modules request navigation.
function refreshView() { return panelNavigation.refresh() }
function openView(view) { return panelNavigation.open(view) }
function initNavigation() { panelNavigation.init() }
