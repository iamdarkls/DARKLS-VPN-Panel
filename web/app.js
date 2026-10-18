'use strict';
let state, original, revision, csrf = '',
  busy = false,
  editing = -1,
  currentView = 'sites',
  statusBusy = false,
  deviceData = [],
  lastStatus = {},
  viewBusy = false;
const $ = id => document.getElementById(id),
  clone = obj => JSON.parse(JSON.stringify(obj));
const categories = {
  video: 'Видео',
  ai: 'Искусственный интеллект',
  social: 'Соцсети',
  messaging: 'Мессенджеры',
  music: 'Музыка',
  reading: 'Чтение и авторы',
  news: 'Новости и СМИ',
  tools: 'Сервисы и инструменты',
  adult: '18+',
  custom: 'Мои группы'
};
const titles = {
  stability: ['Связь и восстановление', 'Причина сбоя, история и безопасные сравнения.'],
  sites: ['Сайты и сервисы', 'Выбирай, что открывать через твой VPN.'],
  devices: ['Устройства', 'Определи, какие устройства используют выбранные маршруты.'],
  diagnostics: ['Диагностика', 'Соединение, DNS и маршрутизация сайтов.'],
  metrics: ['Ресурсы роутера', 'Фактическая память, CPU и свободное место.'],
  logs: ['Журнал событий', 'Что менялось и почему восстанавливался VPN.'],
  backups: ['Резервные копии', 'Возвращайся к предыдущим настройкам.'],
  settings: ['Настройки панели', 'Пароль, перенос настроек и обновления.']
};
const icons = {
  youtube: '▶',
  chatgpt: '✳',
  instagram: '◎',
  discord: '◒',
  gemini: '✦',
  twitter: '𝕏',
  telegram: '➤',
  custom: '+'
};

function el(tag, className, text) {
  const n = document.createElement(tag);
  if (className) n.className = className;
  if (text !== undefined) n.textContent = text;
  return n
}
async function api(path, method = 'GET', body) {
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method === 'POST') headers['X-CSRF-Token'] = csrf;
  const controller = new AbortController(),
    timer = setTimeout(() => controller.abort(), path === 'status' ? 8000 : 180000);
  try {
    const res = await fetch('/api/' + path, {
      method,
      headers,
      credentials: 'same-origin',
      signal: controller.signal,
      body: body === undefined ? undefined : JSON.stringify(body)
    });
    let data;
    try {
      data = await res.json()
    } catch (e) {
      if (e.name === 'AbortError') throw e;
      throw new Error('Нет ответа от панели. Проверь подключение к роутеру.')
    }
    if (!res.ok) {
      if (res.status === 401 && path !== 'login') showLogin();
      throw new Error(data.error || 'Ошибка запроса')
    }
    return data;
  } catch (e) {
    if (e.name === 'AbortError') throw new Error(
      'Панель не ответила вовремя. Обнови страницу перед повторным применением.');
    if (e instanceof TypeError) throw new Error('Нет связи с панелью. Проверь подключение к роутеру.');
    throw e
  } finally {
    clearTimeout(timer)
  }
}

function showLogin() {
  $('password-form').reset();
  $('update-password').value = '';
  $('github-password').value = '';
  document.querySelectorAll('[data-secret]').forEach(n => n.value = '');
  $('login').classList.remove('hidden');
  $('app').classList.add('hidden');
  csrf = ''
}

function notice(text, error = false) {
  $('notice').textContent = text;
  $('notice').classList.toggle('error', error);
  $('notice').classList.toggle('hidden', !text)
}

function dirty() {
  return !!state && JSON.stringify(state) !== JSON.stringify(original)
}

function updateSavebar() {
  const changed = dirty();
  $('apply').disabled = !changed || busy;
  $('discard').disabled = !changed || busy;
  $('save-title').textContent = busy ? 'Выполняем операцию…' : changed ? 'Есть несохранённые изменения' :
    'Все изменения сохранены';
  $('save-subtitle').textContent = busy ? 'Дождись завершения.' : changed ?
    'Они вступят в силу после нажатия «Применить».' : 'Настройки действуют для домашней сети.'
}

function setBusy(v) {
  busy = v;
  document.querySelectorAll(
    '#groups button,#add-group,#power,.restore-button,#devices-list input,#device-mode').forEach(b => b
    .disabled = v);
  updateSavebar()
}
for (const [id, label] of Object.entries(categories)) {
  for (const select of [$('category-filter'), $('group-category')]) {
    const opt = el('option', '', label);
    opt.value = id;
    select.append(opt)
  }
}
const all = el('option', '', 'Все назначения');
all.value = '';
$('category-filter').prepend(all);
$('category-filter').value = '';

function render() {
  const q = $('search').value.toLowerCase().trim(),
    filter = $('category-filter').value;
  $('groups').replaceChildren();
  let domains = new Set;
  state.groups.filter(g => g.enabled).forEach(g => g.domains.forEach(d => domains.add(d)));
  $('count').textContent = state.groups.filter(g => g.enabled).length + ' групп включено · ' + domains.size +
    ' доменов';
  for (const [category, label] of Object.entries(categories)) {
    const matches = state.groups.map((g, i) => ({
      g,
      i
    })).filter(({
        g
      }) => (g.category || 'custom') === category && (!filter || filter === category) && (!q || g.name
        .toLowerCase().includes(q) || label.toLowerCase().includes(q) || g.domains.some(d => d.includes(q))
        ));
    if (!matches.length) continue;
    const section = el('section', 'purpose-section'),
      heading = el('div', 'purpose-heading');
    heading.append(el('h2', '', label), el('span', 'muted tiny', matches.length + ' групп'));
    section.append(heading);
    const grid = el('div', 'groups-grid');
    for (const {
        g,
        i
      }
      of matches) {
      const card = el('article', 'card' + (g.enabled ? '' : ' disabled')),
        head = el('div', 'card-head');
      head.append(el('div', 'service-icon', icons[g.id] || (g.id.startsWith('adult-') ? '18+' : g.name.slice(
        0, 1).toUpperCase())));
      const title = el('div', 'card-title');
      title.append(el('h3', '', g.name), el('div', 'card-meta', g.domains.length + ' доменов' + (g.id ===
        'telegram' ? ' + IP-сети' : '')));
      head.append(title);
      const toggle = el('button', 'switch' + (g.enabled ? ' on' : ''));
      toggle.type = 'button';
      toggle.setAttribute('role', 'switch');
      toggle.setAttribute('aria-checked', String(g.enabled));
      toggle.setAttribute('aria-label', g.name);
      toggle.disabled = busy;
      toggle.onclick = () => {
        g.enabled = !g.enabled;
        g.enabled_until = 0;
        g.timed_previous = false;
        render()
      };
      head.append(toggle);
      card.append(head, el('div', 'domain-preview', g.domains.slice(0, 5).join(' · ') ||
        'Добавь домены этой группы'));
      const bottom = el('div', 'card-bottom');
      bottom.append(el('span', 'route-tag', g.enabled ? 'ЧЕРЕЗ VPN' : 'ГРУППА ВЫКЛЮЧЕНА'));
      const edit = el('button', 'edit-button', 'Настроить ↗');
      edit.disabled = busy;
      edit.onclick = () => editGroup(i);
      bottom.append(edit);
      card.append(bottom);
      if (g.enabled_until || g.quic_until) card.append(el('p', 'tiny muted', 'До ' + stamp(Math.max(g
        .enabled_until || 0, g.quic_until || 0)) + ' · затем возврат прежнего выбора'));
      const timer = el('button', 'link-button', 'Включить на час');
      timer.disabled = busy;
      timer.onclick = () => action(timer, () => runGroupTimer(g.id, 60));
      card.append(timer);
      grid.append(card);
    }
    section.append(grid);
    $('groups').append(section);
  }
  if (!$('groups').children.length) $('groups').append(el('div', 'empty', 'Ничего не найдено'));
  updateSavebar()
}

function editGroup(index) {
  editing = index;
  const g = index < 0 ? {
    name: '',
    domains: [],
    category: 'custom'
  } : state.groups[index];
  $('group-quic').checked = g.block_quic ?? g.id === 'youtube';
  $('group-name').value = g.name;
  $('group-domains').value = g.domains.join('\n');
  $('group-category').value = g.category || 'custom';
  $('edit-title').textContent = index < 0 ? 'Новая группа' : 'Домены сервиса';
  $('delete-group').classList.toggle('hidden', index < 0);
  $('edit-dialog').showModal();
  $('group-name').focus()
}
$('edit-form').addEventListener('submit', event => {
  event.preventDefault();
  const name = $('group-name').value.trim(),
    domains = $('group-domains').value.split(/\r?\n/).map(d => d.trim()).filter(Boolean),
    category = $('group-category').value;
  if (!name) return;
  const block_quic = $('group-quic').checked;
  if (editing < 0) state.groups.push({
    id: 'custom-' + Array.from(crypto.getRandomValues(new Uint8Array(8)), b => b.toString(16)
      .padStart(2, '0')).join(''),
    name,
    enabled: true,
    domains,
    category,
    block_quic
  });
  else Object.assign(state.groups[editing], {
    name,
    domains,
    category,
    block_quic,
    ...((state.groups[editing].block_quic ?? state.groups[editing].id === 'youtube') !== block_quic ?
    {
      quic_until: 0,
      quic_previous: false
    } : {})
  });
  $('edit-dialog').close();
  render()
});
$('close-dialog').onclick = () => $('edit-dialog').close();
$('delete-group').onclick = () => {
  if (confirm('Удалить группу «' + state.groups[editing].name + '»?')) {
    state.groups.splice(editing, 1);
    $('edit-dialog').close();
    render()
  }
};
$('add-group').onclick = () => editGroup(-1);
$('search').addEventListener('input', render);
$('category-filter').onchange = render;
async function loadState() {
  const data = await api('state');
  state = data.state;
  if (!state.policy) state.policy = {
    mode: 'all',
    devices: []
  };
  original = clone(state);
  revision = data.revision;
  render();
  renderDevices()
}

function stamp(t) {
  return t > 1735689600 ? new Date(t * 1000).toLocaleString('ru-RU') : 'Ещё нет данных'
}

function facts(container, rows) {
  container.replaceChildren();
  for (const [label, value] of rows) {
    const row = el('div', 'fact');
    row.append(el('span', 'muted', label), el('strong', '', String(value)));
    container.append(row)
  }
}

function showProbe(p = {}, fresh = false) {
  facts($('probe-details'), [
    ['HTTPS через VPN', fresh ? (p.ok ? 'Запрос прошёл' : 'Ошибка запроса') : 'Нет свежей проверки'],
    ['Последний успех', stamp(p.last_success)],
    ['Последняя ошибка', stamp(p.last_failure)],
    ['Последовательных неудач', p.failures || 0],
    ['Восстановлений с перезагрузки', p.recoveries || 0],
    ['Последнее восстановление', stamp(p.last_recovery)],
    ['Причина', p.recovery_reason || '—'],
    ['Длительность проверки', p.duration_ms === undefined ? '—' : p.duration_ms + ' мс'],
    ['Ошибка', p.error || '—']
  ])
}
async function status() {
  if (!csrf || busy || statusBusy) return;
  statusBusy = true;
  try {
    const s = await api('status');
    lastStatus = s;
    const active = s.active;
    $('connection').textContent = active ? '● VPN работает' : s.disabled ? '○ VPN выключен' :
      '◌ Требуется проверка';
    $('connection').classList.toggle('off', !active);
    $('vpn-light').classList.toggle('on', active);
    $('vpn-title').textContent = active ? 'Соединение через VPN подтверждено' : s.disabled ?
      'Маршрутизация выключена' : s.routes ? 'Маршруты подняты, проверяем соединение' :
      'Маршрутизация восстанавливается';
    $('vpn-desc').textContent = active ? 'Последняя успешная HTTPS-проверка: ' + stamp(s.probe
        ?.last_success) : s.disabled ? 'Сайты открываются через обычное подключение.' : s.probe?.error ||
      'Ожидаем свежую HTTPS-проверку автоконтроля.';
    $('power').textContent = s.disabled ? 'Включить VPN' : 'Выключить VPN';
    $('power').dataset.enabled = String(!s.disabled);
    $('checks').replaceChildren();
    for (const [key, label] of [
        ['xray', 'Xray'],
        ['dns', 'DNS'],
        ['nat', 'TCP'],
        ['udp', 'UDP'],
        ['watch', 'Автоконтроль']
      ]) $('checks').append(el('span', 'check' + (s[key] ? '' : ' bad'), (s[key] ? '✓ ' : '○ ') + label));
    $('checks').append(el('span', 'check' + (s.probe_fresh && s.probe?.ok ? '' : ' bad'), (s.probe_fresh &&
      s.probe?.ok ? '✓ ' : '○ ') + 'HTTPS'));
    showProbe(s.probe, s.probe_fresh)
  } catch (e) {
    notice(e.message, true)
  } finally {
    statusBusy = false
  }
}
async function loadDevices() {
  const data = await api('devices');
  deviceData = data.devices;
  renderDevices()
}

function renderDevices() {
  if (!state?.policy) return;
  $('device-mode').value = state.policy.mode;
  const selected = new Set(state.policy.devices.map(d => d.mac));
  $('devices-list').replaceChildren();
  const allDevices = [...deviceData];
  for (const ref of state.policy.devices)
    if (!allDevices.some(d => d.mac === ref.mac)) allDevices.push(ref);
  for (const d of allDevices) {
    const row = el('label', 'device-row'),
      check = el('input');
    check.type = 'checkbox';
    check.checked = selected.has(d.mac);
    check.disabled = busy || state.policy.mode === 'all';
    check.setAttribute('aria-label', d.name || d.ip || d.mac);
    check.onchange = () => {
      state.policy.devices = state.policy.devices.filter(x => x.mac !== d.mac);
      if (check.checked) state.policy.devices.push({
        mac: d.mac,
        name: d.name || ''
      });
      renderDevices();
      updateSavebar()
    };
    const info = el('span');
    info.append(el('strong', '', d.name || 'Устройство'), el('span', 'tiny muted', (d.ip ||
      'IP пока не найден') + ' · ' + d.mac + ' · ' + (d.online ? 'видно в ARP' : 'известно роутеру')));
    row.append(check, info);
    $('devices-list').append(row)
  }
  if (!allDevices.length) $('devices-list').append(el('div', 'empty',
    'Пока нет устройств в DHCP/ARP. Подключи устройство к домашней сети и обнови список.'));
  $('device-summary').textContent = state.policy.mode === 'all' ? 'VPN применяется ко всей домашней сети.' :
    'Выбрано устройств: ' + selected.size;
  updateSavebar()
}
$('device-mode').onchange = () => {
  state.policy.mode = $('device-mode').value;
  renderDevices()
};
$('refresh-devices').onclick = () => action($('refresh-devices'), loadDevices);
async function loadMetrics() {
  const m = await api('metrics');
  $('metrics-grid').replaceChildren();
  const mib = k => k === undefined ? '—' : (k / 1024).toFixed(1) + ' МиБ',
    bytes = b => (b / 1048576).toFixed(1) + ' МиБ';
  const values = [
    ['Процессор', m.cpu_percent === null ? 'CPU недоступен' : m.cpu_percent.toFixed(1) + '%', 'Load: ' + (
      m.load || []).join(' / ')],
    ['Доступная RAM', mib(m.memory_available_kib), 'Всего: ' + mib(m.memory_total_kib)],
    ['Панель · RAM', mib(m.processes?.panel?.rss_kib), 'Swap: ' + mib(m.processes?.panel?.swap_kib)],
    ['Xray · RAM', mib(m.processes?.xray?.rss_kib), 'Swap: ' + mib(m.processes?.xray?.swap_kib)],
    ['Свободно /jffs', bytes(m.jffs_free_bytes), 'Всего: ' + bytes(m.jffs_total_bytes)],
    ['Время работы', Math.floor(m.uptime_seconds / 3600) + ' ч ' + Math.floor(m.uptime_seconds % 3600 /
      60) + ' мин', 'USB: ' + (m.usb ? 'подключена' : 'не обнаружена')]
  ];
  for (const [label, value, sub] of values) {
    const card = el('div', 'metric-card');
    card.append(el('span', 'muted', label), el('strong', '', value), el('span', 'tiny muted', sub));
    $('metrics-grid').append(card)
  }
}
async function loadLogs() {
  const d = await api('logs');
  $('event-list').replaceChildren();
  for (const e of d.events) {
    const row = el('div', 'event-row');
    row.append(el('span', 'tiny muted', stamp(e.at)), el('strong', '', e.message));
    $('event-list').append(row)
  }
  if (!d.events.length) $('event-list').append(el('p', 'muted', 'Событий пока нет.'));
  $('watch-log').textContent = d.watch || 'Записей пока нет.';
  $('panel-log').textContent = d.panel || 'Записей пока нет.';
  $('update-log').textContent = d.update || 'Записей пока нет.'
}
$('refresh-logs').onclick = () => action($('refresh-logs'), loadLogs);
async function action(button, fn) {
  button.disabled = true;
  try {
    await fn()
  } catch (e) {
    notice(e.message, true)
  } finally {
    button.disabled = button.dataset.locked === 'true'
  }
}
$('probe-now').onclick = () => action($('probe-now'), async () => {
  const p = await api('probe', 'POST', {});
  showProbe(p, true);
  await status()
});
$('diagnose-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    const d = await api('diagnose', 'POST', {
      domain: $('diagnose-domain').value
    });
    $('diagnose-result').replaceChildren();
    const box = el('div', 'facts');
    facts(box, [
      ['Домен', d.domain],
      ['Это устройство', d.client_ip],
      ['Разрешено режимом устройств', d.device_eligible ? 'Да' : 'Нет'],
      ['Правила роутера', d.routes_active ? 'Подключены' : 'Не подключены'],
      ['Группы', d.groups.map(g => g.name + (g.enabled ? ' · вкл.' : ' · выкл.')).join(', ') ||
        'Совпадений нет'
      ]
    ]);
    $('diagnose-result').append(box);
    for (const a of d.addresses) $('diagnose-result').append(el('p', 'route-result', a.ip + ' → ' + (a
        .via_vpn ? 'По правилам должен идти через VPN' : 'По проверенным правилам VPN не выбран'
        ) + (a.in_vpn_set ? ' · IP есть в VPN-наборе' : '')));
  })
};
async function loadBackups() {
  const data = await api('backups');
  $('backups').replaceChildren();
  for (const id of data.backups) {
    const row = el('div', 'backup-row'),
      info = el('div');
    const date = id.slice(0, 8) + ' ' + id.slice(9, 11) + ':' + id.slice(11, 13) + ':' + id.slice(13, 15);
    info.append(el('strong', '', date), el('span', 'tiny', id));
    row.append(info);
    const button = el('button', 'secondary restore-button', 'Восстановить');
    button.disabled = busy;
    button.onclick = async () => {
      if (dirty() && !confirm('Есть несохранённые изменения. Продолжить?')) return;
      if (!confirm('Восстановить копию ' + date + '?')) return;
      setBusy(true);
      try {
        await api('restore', 'POST', {
          backup: id,
          revision
        });
        await loadState();
        notice(
          'Резервная копия восстановлена. Прежние IP удалённых доменов могут сохраняться до 24 часов.'
          );
        await loadBackups()
      } catch (e) {
        notice(e.message, true)
      } finally {
        setBusy(false);
        await status()
      }
    };
    row.append(button);
    $('backups').append(row)
  }
  if (!data.backups.length) $('backups').append(el('div', 'empty',
    'Копии появятся после первого применения настроек.'))
}
async function refreshView() {
  if (viewBusy) return;
  viewBusy = true;
  try {
    if (currentView === 'devices') await loadDevices();
    if (currentView === 'metrics') await loadMetrics();
    if (currentView === 'logs') await loadLogs();
    if (currentView === 'backups') await loadBackups();
    if (currentView === 'settings') {
      await loadGitHub();
      await loadAdvancedSettings();
      await loadCore()
    }
    if (currentView === 'stability') await loadServiceHistory();
    if (currentView === 'backups') await loadStorage()
  } catch (e) {
    notice(e.message, true)
  } finally {
    viewBusy = false
  }
}
document.querySelectorAll('.nav').forEach(button => button.onclick = async () => {
  currentView = button.dataset.view;
  document.querySelectorAll('.nav').forEach(b => b.classList.toggle('active', b === button));
  for (const key of Object.keys(titles)) $(key + '-view').classList.toggle('hidden', currentView !==
    key);
  $('page-title').textContent = titles[currentView][0];
  $('page-subtitle').textContent = titles[currentView][1];
  await refreshView()
});
$('discard').onclick = () => {
  state = clone(original);
  render();
  renderDevices();
  notice('')
};
$('apply').onclick = () => previewApply();
$('power').onclick = async () => {
  const enabled = $('power').dataset.enabled !== 'true';
  if (!enabled && !confirm('Выключить VPN для домашней сети?')) return;
  setBusy(true);
  try {
    await api('power', 'POST', {
      enabled
    });
    notice(enabled ? 'VPN включён.' : 'VPN выключен. Автоконтроль сохранит этот выбор.')
  } catch (e) {
    notice(e.message, true)
  } finally {
    setBusy(false);
    await status()
  }
};
$('password-form').onsubmit = event => {
  event.preventDefault();
  const form = event.target,
    data = new FormData(form);
  if (data.get('password') !== data.get('confirm')) {
    notice('Новые пароли не совпадают.', true);
    return
  }
  if (dirty() && !confirm(
      'Есть несохранённые изменения. После смены пароля потребуется повторный вход. Продолжить?')) return;
  action(form.querySelector('button'), async () => {
    await api('password', 'POST', {
      current: data.get('current'),
      password: data.get('password')
    });
    form.reset();
    state = undefined;
    original = undefined;
    showLogin();
    $('login-error').textContent = 'Пароль изменён. Войди с новым паролем.'
  })
};
$('import-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    const file = $('import-file').files[0];
    if (!file || file.size > 512 * 1024) throw new Error('Выбери JSON размером до 512 КиБ.');
    if (dirty() && !confirm('Импорт заменит несохранённые изменения. Продолжить?')) return;
    let input;
    try {
      input = JSON.parse(await file.text())
    } catch {
      throw new Error('Файл не является корректным JSON.')
    }
    const d = await api('import-preview', 'POST', input);
    state = d.state;
    render();
    renderDevices();
    notice('Импорт просмотрен: ' + d.groups + ' групп, ' + d.domains +
      ' активных доменов. Нажми «Применить изменения», чтобы сохранить.')
  })
};
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
$('update-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    if (dirty()) throw new Error('Сначала примени или отмени изменения списков.');
    const file = $('update-file').files[0];
    if (!file || file.size > 8 * 1024 * 1024) throw new Error('Выбери пакет ZIP размером до 8 МиБ.');
    const form = new FormData();
    form.append('password', $('update-password').value);
    form.append('package', file);
    setBusy(true);
    $('update-progress').textContent = 'Загружаем и проверяем пакет…';
    let accepted = false;
    try {
      const response = await fetch('/api/update', {
        method: 'POST',
        headers: {
          'X-CSRF-Token': csrf
        },
        credentials: 'same-origin',
        body: form
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || 'Не удалось загрузить пакет');
      accepted = true;
      $('update-password').value = '';
      await waitForPanelUpdate(data.job, $('update-progress'))
    } finally {
      setBusy(false);
      if (!accepted) $('update-progress').textContent = 'Пакет не установлен.'
    }
  })
};
$('logout').onclick = async () => {
  if (dirty() && !confirm('Выйти без сохранения?')) return;
  try {
    await api('logout', 'POST', {})
  } finally {
    state = undefined;
    original = undefined;
    showLogin()
  }
};
async function openApp() {
  const session = await api('session');
  csrf = session.csrf;
  $('version').textContent = 'v' + session.version;
  $('login').classList.add('hidden');
  $('app').classList.remove('hidden');
  await loadState();
  await status()
}
$('login-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    const form = new FormData(event.target);
    try {
      await api('login', 'POST', {
        user: form.get('user'),
        password: form.get('password')
      });
      event.target.elements.password.value = '';
      $('login-error').textContent = '';
      await openApp()
    } catch (e) {
      $('login-error').textContent = e.message
    }
  })
};
window.addEventListener('beforeunload', e => {
  if (dirty()) {
    e.preventDefault();
    e.returnValue = ''
  }
});
setInterval(() => {
  if (!document.hidden && csrf && !busy) {
    status();
    if (['metrics', 'logs', 'devices'].includes(currentView)) refreshView()
  }
}, 30000);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && csrf) {
    status();
    refreshView()
  }
});
try {
  const message = localStorage.getItem('darkls-update-message');
  if (message) {
    $('login-error').textContent = message;
    localStorage.removeItem('darkls-update-message')
  }
} catch {}
openApp().catch(() => showLogin());

$('service-probe-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    const d = await api('service-probe', 'POST', {
      service: $('service-probe-select').value
    });
    facts($('service-probe-result'), [
      ['Сервис', d.domain],
      ['Результат', d.message],
      ['HTTP', d.http || '—'],
      ['Время', d.duration_ms + ' мс']
    ]);
  })
};

let githubVersion = '';

function renderGitHub(d) {
  githubVersion = d.available && d.signed ? d.latest : '';
  facts($('github-status'), [
    ['Установлена', d.current],
    ['Последний выпуск', d.latest || '—'],
    ['Подпись', d.signed ? 'Подтверждена' : d.configured ? 'Не проверена' :
      'Сначала настройте свой репозиторий'
    ],
    ['Проверено', stamp(d.checked_at)],
    ['Состояние', d.error || (!d.configured ?
      'Первый Release из своего репозитория нужно установить ZIP-файлом.' : !d.checked_at ?
      'Проверка ещё не выполнялась' : d.available ? 'Доступно обновление' : 'Обновлений нет')]
  ]);
  $('github-notes').textContent = d.notes || '';
  $('github-install-form').classList.toggle('hidden', !githubVersion)
}
async function loadGitHub() {
  renderGitHub(await api('github'))
}
$('github-check').onclick = () => action($('github-check'), async () => {
  renderGitHub(await api('github-check', 'POST', {}))
});
// Both ZIP upload and GitHub updates follow the same restart-aware job polling.
async function waitForPanelUpdate(job, element) {
  const deadline = Date.now() + 180000;
  while (Date.now() < deadline) {
    await sleep(1000);
    const controller = new AbortController(),
      timer = setTimeout(() => controller.abort(), 4000);
    let d;
    try {
      const response = await fetch('/api/update-status', {
        cache: 'no-store',
        signal: controller.signal
      });
      d = await response.json()
    } catch {
      continue
    } finally {
      clearTimeout(timer)
    }
    if (d.job?.id !== job.id) continue;
    element.textContent = d.job.message;
    if (d.job.phase === 'succeeded') {
      try {
        localStorage.setItem('darkls-update-message', 'Панель обновлена. Войди снова.')
      } catch {}
      location.reload();
      return
    }
    if (['error', 'rolled_back'].includes(d.job.phase)) throw new Error(d.job.message)
  }
  throw new Error('Ожидание завершено. Обнови страницу и проверь версию панели.')
}
$('github-install-form').onsubmit = event => {
  event.preventDefault();
  action(event.target.querySelector('button'), async () => {
    if (dirty()) throw new Error('Сначала примени или отмени изменения списков.');
    if (!githubVersion) throw new Error('Сначала проверь обновления.');
    setBusy(true);
    $('github-progress').textContent = 'Загружаем выпуск и проверяем подпись…';
    try {
      const d = await api('github-install', 'POST', {
        password: $('github-password').value,
        version: githubVersion
      });
      $('github-password').value = '';
      await waitForPanelUpdate(d.job, $('github-progress'))
    } finally {
      setBusy(false)
    }
  })
};
