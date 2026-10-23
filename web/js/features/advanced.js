'use strict';
// All remote values are rendered through textContent. Connection credentials
// are sent only to the router and never retained in browser storage.
// Background refresh may update status, but must preserve an edited form.
// A new login discards the previous session's draft on its first refresh.
function cleanDraft() {
  if (dirty()) throw new Error('Сначала примени или отмени изменения списков.')
}
async function runGroupTimer(group, minutes, quic) {
  cleanDraft();
  await api('timer', 'POST', {
    group,
    minutes,
    revision,
    ...(quic === undefined ? {} : {
      quic
    })
  });
  await loadState();
  notice('Таймер сохранён. После срока вернётся прежнее состояние; роутер должен синхронизировать часы.');
  await loadServiceHistory()
}
const stateLabel = s => ({
  responded: 'Сайт ответил',
  challenge: 'Проверка Cloudflare',
  timeout: 'Тайм-аут',
  dns_error: 'Ошибка DNS',
  connection_error: 'Ошибка соединения',
  http_error: 'HTTP-ошибка'
} [s] || s);
const recovery = $('stability-recovery');
const diagnostics = $('diagnostics-view');
const tuning = $('tuning-view');
const connectivityBox = box(diagnostics, 'Что именно не отвечает',
  'Прямой HTTPS, TCP VPN-сервера, DNS, правила и общий туннель проверяются отдельно. Неудача одного сайта не перезапускает VPN. Проверка прямого HTTPS через одного провайдера может не подтвердить доступ, даже если часть интернета работает.'
  );
const connectivityResult = output(connectivityBox);
button(connectivityBox, 'Проверить связь', async () => {
  connectivityResult.textContent = 'Проверяем, до 40 секунд…';
  const c = await api('connectivity', 'POST', {});
  const rows = [
    ['Часы', c.clock ? 'Синхронизированы' : 'Ожидаем NTP'],
    ['Xray', c.xray],
    ['Процесс DNS helper', c.dns_process],
    ['DNS helper', c.local_dns],
    ['DNS Xray', c.tunnel_dns],
    ['Правила', c.rules],
    ['Прямой HTTPS', c.traffic_checked ? c.direct_internet : 'Не проверен'],
    ['TCP VPN-сервера', c.traffic_checked ? c.server_tcp : 'Не проверен'],
    ['HTTPS через VPN', c.traffic_checked ? c.tunnel_https : 'Не проверен']
  ].map(([k, v]) => [k, typeof v === 'boolean' ? (v ? 'Ответил / исправен' : 'Не подтверждён') : v]);
  facts(connectivityResult, rows);
  for (const [k, v] of Object.entries(c.errors || {})) connectivityResult.append(el('p', 'tiny muted',
    k + ': ' + v))
});
const reportLink = el('a', 'secondary download', 'Скачать отчёт о сбое ↓');
reportLink.href = '/api/report';
connectivityBox.append(reportLink, el('p', 'tiny muted',
  'JSON: версии, память, правила, результаты проверок и последние ошибки. Пароли и VPN-ключи исключены, IP и MAC заменены метками. История выбранных проверок остаётся. Сбор может занять до 40 секунд.'
  ));
const pendingBox = box(recovery, 'Завершить незавершённую операцию', 'Если панель сообщает о режиме восстановления, подключи USB с резервной копией и повтори откат. Другие изменения будут доступны после успешного восстановления.');
const pendingPassword = field(pendingBox, 'Пароль для завершения восстановления', 'password');
button(pendingBox, 'Повторить восстановление', async () => {
  try {
    await api('recovery', 'POST', {password: pendingPassword.value});
    notice('Восстановление завершено');
    await loadState();
    await status();
  } finally { pendingPassword.value = '' }
});
const recoveryBox = box(recovery, 'Восстановить отдельную часть',
  'Автоконтроль сначала чинит процессы, DNS helper и правила. Xray перезапускается только при неисправности его туннеля или DNS-входа. Подключения могут оборваться при переключении канала и перезапуске Xray.'
  );
for (const [component, label] of [
    ['auto', 'Найти и восстановить'],
    ['dns', 'Только DNS helper'],
    ['rules', 'Только правила']
  ]) button(recoveryBox, label, async () => {
  cleanDraft();
  await api('recover-part', 'POST', {
    component
  });
  notice('Проверка / восстановление выполнены. Подробности в журнале.');
  await status()
});
const xrayPassword = field(recoveryBox, 'Пароль для ручного перезапуска Xray', 'password');
button(recoveryBox, 'Перезапустить Xray', async () => {
  cleanDraft();
  if (!confirm('Текущие соединения через VPN переподключатся. Продолжить?')) return;
  await api('recover-part', 'POST', {
    component: 'xray',
    password: xrayPassword.value
  });
  xrayPassword.value = '';
  notice('Xray перезапущен. DNS helper и правила сохранены.')
});
const dependencyBox = box(diagnostics, 'Зависимости сервиса',
  'Проверка известных доменов: DNS, включённая группа и IP в VPN-наборе. HTTPS основной страницы проверяется отдельно. Это проверка с роутера; учётная запись, WebRTC и браузер не проверяются.'
  );
const knownServices = [
  ['instagram', 'Instagram'],
  ['chatgpt', 'ChatGPT'],
  ['youtube', 'YouTube'],
  ['discord', 'Discord'],
  ['gemini', 'Gemini'],
  ['twitter', 'X / Twitter'],
  ['grok', 'Grok / xAI'],
  ['telegram', 'Telegram']
];
const dependencySelect = select(dependencyBox, knownServices, 'Сервис'),
  dependencyResult = output(dependencyBox);
button(dependencyBox, 'Проверить зависимости', async () => {
  dependencyResult.textContent = 'Проверяем DNS и HTTPS…';
  const d = await api('dependencies', 'POST', {
    service: dependencySelect.value
  });
  showRows(dependencyResult, d.rows.map(r => ({
    domain: r.domain,
    dns: r.dns,
    route: r.configured ? 'Выбран группой: ' + r.groups.join(', ') :
      'Нет включённого доменного правила',
    ip: r.addresses.join(', ') || 'Нет A-записи',
    set: r.in_vpn_set ? 'Да' : 'Нет',
    https: r.https?.message || 'Не проверен'
  })), [
    ['domain', 'Домен'],
    ['dns', 'DNS'],
    ['route', 'Настройка маршрута'],
    ['set', 'IP в VPN-наборе'],
    ['https', 'HTTPS через VPN'],
    ['ip', 'IPv4']
  ]);
  dependencyResult.append(el('p', '', d.https.message), el('p', 'tiny muted', d.note));
  await loadServiceHistory()
});
const historyBox = box($('stability-history'), 'История состояния сервисов',
  'Периодические проверки включённых известных сервисов. Ошибка DNS, тайм-аут, ответ сайта и Cloudflare показаны отдельно. Пустое состояние туннеля означает отсутствие свежего общего замера.'
  );
const historyResult = output(historyBox);
button(historyBox, 'Обновить историю', loadServiceHistory);
async function loadServiceHistory() {
  const d = await api('service-history');
  const rows = Object.values(d.current || {}).map(r => ({
    service: r.service,
    state: stateLabel(r.state),
    since: stamp(r.since),
    at: stamp(r.at),
    tunnel: r.tunnel_ok === null ? 'Нет свежего замера' : r.tunnel_ok ? 'Работал' : 'Ошибка'
  }));
  showRows(historyResult, rows, [
    ['service', 'Сервис'],
    ['state', 'Последний результат'],
    ['since', 'Состояние с'],
    ['at', 'Проверено'],
    ['tunnel', 'Общий туннель']
  ]);
  const details = el('details'),
    summary = el('summary', '', 'Последние изменения и длительность');
  details.append(summary);
  for (const r of (d.events || []).filter(r => r.previous).slice(-30).reverse()) details.append(el('p',
    'tiny', stamp(r.at) + ' · ' + r.service + ': ' + stateLabel(r.previous) + ' → ' + stateLabel(r
      .state) + ' · прежнее состояние ' + r.previous_duration_seconds + ' с'));
  historyResult.append(details, el('p', 'tiny muted', d.retention))
}
const quicBox = box(tuning, 'Сравнить QUIC и TCP для группы',
  'HTTPS-тесты панели используют TCP и не измеряют QUIC. Для честного сравнения включи пробный режим на 5 минут, перезапусти приложение на устройстве и открой тот же ролик/сервис. Затем оцени результат. Таймер вернёт прежний выбор; рабочий вариант можно сохранить в настройках группы.'
  );
const quicGroup = select(quicBox, knownServices, 'Группа'),
  quicMode = select(quicBox, [
    ['true', 'Запретить QUIC, использовать TCP'],
    ['false', 'Разрешить QUIC']
  ], 'Пробный режим');
button(quicBox, 'Включить пробный режим на 5 минут', () => runGroupTimer(quicGroup.value, 5, quicMode
  .value === 'true'));
for (const [verdict, label] of [
    ['good', 'Работает нормально'],
    ['slow', 'Медленно'],
    ['failed', 'Не работает']
  ]) button(quicBox, label, async () => {
  await api('quic-observation', 'POST', {
    group: quicGroup.value,
    verdict
  });
  notice('Оценка сохранена в истории.');
  await loadServiceHistory()
});
const timerBox = box($('sites-timer-content'), 'Временно включить группу',
  'Кнопка «Включить на час» есть на карточке каждой группы. Здесь можно выбрать другой срок. Прежнее состояние вернётся автоматически, в том числе после перезагрузки и синхронизации часов. Для устройств сохраняется общий выбранный режим.'
  );
const timerGroup = select(timerBox, [], 'Группа'),
  timerMinutes = field(timerBox, 'Срок в минутах, от 1 до 1440', 'number');
timerMinutes.min = 1;
timerMinutes.max = 1440;
timerMinutes.value = 60;
button(timerBox, 'Включить временно', () => runGroupTimer(timerGroup.value, Number(timerMinutes.value)));
const originalRender = render;
render = function() {
  originalRender();
  if (typeof timerGroup !== 'undefined') {
    const selected = timerGroup.value;
    timerGroup.replaceChildren();
    for (const g of state.groups) {
      const o = el('option', '', g.name);
      o.value = g.id;
      timerGroup.append(o)
    }
    if (state.groups.some(g => g.id === selected)) timerGroup.value = selected
  }
};
const xhttpBox = box(tuning, 'Сравнить параметры XHTTP',
  'Текущие параметры, 2 и 4 соединения сравниваются последовательно в отдельном процессе Xray. До 3 МиБ тестовых загрузок плюс HTTPS-пробы; нужен запас 96 МиБ доступной RAM. Рабочий канал не меняется. Короткий тест не подтверждает длительную стабильность — повтори замеры в разное время.'
  );
const xhttpResult = output(xhttpBox);
button(xhttpBox, 'Сравнить три варианта', async () => {
  cleanDraft();
  xhttpResult.textContent = 'Идёт сравнение, до 2 минут…';
  const d = await api('xhttp-benchmark', 'POST', {});
  showRows(xhttpResult, d.results.map(r => ({
    name: r.preset,
    median: r.samples - r.failures > 0 ? r.median_ms.toFixed(0) + ' мс' : 'Нет успешных проб',
    fail: r.failures + '/' + r.samples,
    speed: r.download_bytes ? (r.download_mbps.toFixed(2) + ' Мбит/с') : 'Нет данных',
    error: r.error || '—'
  })), [
    ['name', 'Вариант'],
    ['median', 'Медиана HTTPS'],
    ['fail', 'Ошибки / пробы'],
    ['speed', 'Загрузка 1 МиБ'],
    ['error', 'Результат']
  ]);
  xhttpResult.append(el('p', 'tiny muted', d.note))
});
const xhttpPreset = select(xhttpBox, [
    ['reuse2', '2 соединения'],
    ['reuse4', '4 соединения'],
    ['restore', 'Вернуть исходный XMUX этого канала']
  ], 'Применить после сравнения'),
  tuningPassword = field(xhttpBox, 'Текущий пароль', 'password');
button(xhttpBox, 'Проверить и применить XHTTP', async () => {
  cleanDraft();
  if (!confirm(
      'После изолированной проверки Xray перезапустится. Текущие VPN-соединения переподключатся. Продолжить?'
      )) return;
  await api('xhttp-apply', 'POST', {
    preset: xhttpPreset.value,
    password: tuningPassword.value
  });
  tuningPassword.value = '';
  notice('Параметры применены после проверки. При ошибке предусмотрен откат.')
});
const loadBox = box(tuning, 'Задержка под нагрузкой',
  'Ограниченная загрузка до 8 МиБ и 8 секунд. Сравни прямой канал и VPN, затем повтори во время видео на телефоне и скачивания на ПК. Роутер не включает QoS автоматически: сначала нужны измерения вашей линии.'
  );
const loadVia = select(loadBox, [
    ['direct', 'Прямой канал'],
    ['vpn', 'Через VPN']
  ], 'Канал'),
  loadResult = output(loadBox);
button(loadBox, 'Измерить задержку под нагрузкой', async () => {
  loadResult.textContent = 'Измеряем…';
  const d = await api('load-test', 'POST', {
    via: loadVia.value
  });
  facts(loadResult, [
    ['Без нагрузки', d.idle_ms ? d.idle_ms.toFixed(0) + ' мс' : 'Нет успешных проб'],
    ['Под нагрузкой', d.load_ms ? d.load_ms.toFixed(0) + ' мс' : 'Нет успешных проб'],
    ['Ошибки HTTPS', d.failures],
    ['Загружено', (d.bytes / 1048576).toFixed(2) + ' МиБ'],
    ['Выборка достаточна', d.enough_samples ? 'Да' : 'Нет']
  ]);
  loadResult.append(el('p', 'tiny muted', d.note))
});
// Settings are kept local to the router. No endpoint or credentials in GET APIs.
const profileBox = box($('servers-backup'), 'Резервный VPN-канал',
  'Добавь независимый сервер/транспорт. Сначала проверим его отдельным Xray. До успешной проверки резерв не сохраняется; автоматическое переключение по умолчанию выключено. Уже открытые соединения при переключении могут оборваться.'
  );
profileBox.classList.add('wide');
const profileResult = output(profileBox),
  profileSecret = el('textarea');
profileSecret.rows = 4;
profileSecret.placeholder = 'VLESS-ссылка или один outbound JSON';
profileSecret.setAttribute('aria-label', 'Резервное подключение');
profileSecret.dataset.secret = 'true';
profileSecret.autocomplete = 'off';
profileSecret.spellcheck = false;
profileBox.append(profileSecret);
const profilePassword = field(profileBox, 'Текущий пароль панели', 'password');
button(profileBox, 'Проверить и сохранить резерв', async () => {
  cleanDraft();
  try {
    await api('profile-save', 'POST', {
      password: profilePassword.value,
      outbound: profileSecret.value
    });
    profileSecret.value = '';
    notice('Резерв прошёл HTTPS-проверку и сохранён.')
  } finally {
    profilePassword.value = ''
  }
  await loadProfiles()
});
for (const [profile, label] of [
    ['primary', 'Переключиться на основной'],
    ['backup', 'Переключиться на резерв']
  ]) button(profileBox, label, async () => {
  cleanDraft();
  if (!confirm('Проверить выбранный канал и переключить? VPN-соединения переподключатся.')) return;
  try {
    await api('profile-switch', 'POST', {
      profile,
      password: profilePassword.value
    });
    notice('Канал переключён.')
  } finally {
    profilePassword.value = ''
  }
  await loadProfiles()
});
const monitorBox = box($('stability-monitor'), 'Контроль связи',
  'История и ограниченные HTTPS-пробы работают независимо от открытой панели. При отказе одного сервиса Xray не перезапускается. Резерв включается только при подтверждённом отказе общего туннеля и успешной проверке второго канала; автоматического возврата на основной нет.'
  );
monitorBox.classList.add('wide');
const monitorEnabled = check(monitorBox, 'Периодически проверять включённые сервисы'),
  monitorInterval = field(monitorBox, 'Интервал в секундах: 120–3600', 'number'),
  monitorFailover = check(monitorBox, 'Автоматически переключаться на проверенный резерв'),
  monitorPassword = field(monitorBox, 'Текущий пароль', 'password');
// Do not offer failover until the router confirms a configured backup.
monitorFailover.disabled = true;
monitorInterval.min = 120;
monitorInterval.max = 3600;
const monitorDraft = draftFields([monitorEnabled, monitorInterval, monitorFailover]);
button(monitorBox, 'Сохранить контроль связи', async () => {
  await api('preferences', 'POST', {
    password: monitorPassword.value,
    preferences: {
      monitor_services: monitorEnabled.checked,
      interval_seconds: Number(monitorInterval.value),
      auto_failover: monitorFailover.checked
    }
  });
  monitorPassword.value = '';
  monitorDraft.saved();
  notice('Настройки контроля сохранены.');
  await loadMonitorSettings()
});
async function loadMonitorSettings() {
  const [preferences, profiles] = await Promise.all([api('preferences'), api('profiles')]);
  monitorDraft.sync(() => {
    monitorEnabled.checked = preferences.monitor_services;
    monitorInterval.value = preferences.interval_seconds;
    monitorFailover.checked = preferences.auto_failover
  });
  monitorFailover.disabled = !profiles.backup_configured;
}
async function loadProfiles() {
  const profiles = await api('profiles');
  facts(profileResult, [
    ['Активный канал', profiles.active === 'backup' ? 'Резервный' : 'Основной'],
    ['Резерв', profiles.backup_configured ? 'Добавлен и проверен' : 'Не настроен'],
    ['Автопереключение', profiles.auto_failover ? 'Включено' : 'Выключено'],
    ['Последнее переключение', stamp(profiles.last_switch)]
  ])
}
const storageBox = box($('backups-files'), 'Копии и установочные архивы',
  'Показываем доступные известные файлы отдельно от работающей панели и Xray. Текущие программы, настройки и USB-автозапуск удалить здесь нельзя. Прежнее ядро находится на флешке. Размеры файлов указаны без учёта сжатия JFFS.'
  );
const storageResult = output(storageBox),
  storagePassword = field(storageBox, 'Пароль для удаления выбранной копии', 'password');
button(storageBox, 'Обновить список файлов', loadStorage);
async function loadStorage() {
  const d = await api('storage');
  storageResult.replaceChildren();
  for (const item of d.items || []) {
    const row = el('div', 'panel-box');
    row.append(el('strong', '', item.name + ' · ' + (item.bytes / 1048576).toFixed(2) + ' МиБ'), el('p',
      'tiny muted', item.consequence));
    button(row, 'Удалить эту копию', async () => {
      if (!confirm(item.consequence + ' Удалить «' + item.name + '»?')) return;
      await api('storage-delete', 'POST', {
        id: item.id,
        password: storagePassword.value,
        acknowledge: true
      });
      storagePassword.value = '';
      await loadStorage();
      await loadBackups();
      notice('Выбранная копия удалена.')
    }, 'danger');
    storageResult.append(row)
  }
  if (!(d.items || []).length) storageResult.append(el('p', 'muted',
    'Копии и известные архивы не найдены.'));
  return d
}
// Preview is required before committing the exact captured draft/revision.
const previewDialog = el('dialog'),
  previewContent = el('div', 'panel-box');
previewDialog.append(previewContent);
document.body.append(previewDialog);
async function previewApply() {
  setBusy(true);
  try {
    const draft = clone(state),
      expected = revision,
      d = await api('preview', 'POST', {
        state: draft,
        revision: expected
      });
    previewContent.replaceChildren(el('h2', '', 'Перед применением'));
    const previewFacts = el('div', 'facts');
    previewContent.append(previewFacts);
    facts(previewFacts, [
      ['Домены добавятся', d.added_domains.join(', ') || 'Нет'],
      ['Домены уберутся', d.removed_domains.join(', ') || 'Нет'],
      ['Группы', d.groups.join('; ') || 'Без изменений'],
      ['Режим устройств', d.old_device_mode + ' → ' + d.new_device_mode],
      ['Затронутые устройства', d.affected_devices || d.selected_devices],
      ['Перезапуск Xray', d.xray_restart ? 'Да' : 'Не требуется'],
      ['Перезапуск DNS helper', d.dns_reload ? 'Да' : 'Нет']
    ]);
    previewContent.append(el('p', 'muted', d.note));
    button(previewContent, 'Вернуться к редактированию', async () => {
      previewDialog.close();
      setBusy(false)
    });
    button(previewContent, 'Применить эти изменения', async () => {
      previewDialog.close();
      try {
        await api('apply', 'POST', {
          state: draft,
          revision: expected
        });
        await loadState();
        notice(
          'Изменения применены без перезапуска Xray. Для старого DNS-кеша перезапусти приложение или выполни ipconfig /flushdns на ПК.'
          );
        await refreshView()
      } finally {
        setBusy(false);
        await status()
      }
    }, 'primary');
    previewDialog.showModal()
  } catch (e) {
    notice(e.message, true);
    setBusy(false)
  }
}
previewDialog.addEventListener('cancel', () => setBusy(false));
