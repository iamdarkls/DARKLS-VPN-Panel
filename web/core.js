'use strict';
const coreBox = box($('settings-view').querySelector('.settings-grid'), 'Обновления ядра Xray',
  'Официальные стабильные выпуски и пререлизы XTLS/Xray-core для Linux ARM64. Автопроверка раз в сутки; установка по кнопке или в выбранный час МСК. Автоустановка изначально выключена. При замене ядра VPN-соединения кратко переподключатся.'
  );
coreBox.classList.add('wide');
const coreFacts = output(coreBox),
  coreNotes = el('pre', 'log-box'),
  coreProgressText = el('p', 'tiny muted');
coreProgressText.setAttribute('role', 'status');
coreBox.append(coreNotes, coreProgressText);
let coreLatest = '',
  coreBusy = false,
  corePoll = null,
  corePollBusy = false,
  coreLoadRequest = null;
button(coreBox, 'Проверить обновления Xray', async () => {
  const st = await api('xray-check', 'POST', {});
  if (st.error) notice(st.error, true);
  await loadCore()
});
const coreCheckEnabled = check(coreBox, 'Автоматически проверять обновления Xray'),
  coreAutoEnabled = check(coreBox, 'Устанавливать обновления, включая пререлизы, автоматически'),
  coreHour = field(coreBox, 'Час автоустановки, московское время (0–23)', 'number');
coreHour.min = 0;
coreHour.max = 23;
const coreDraft = draftFields([coreCheckEnabled, coreAutoEnabled, coreHour]);
coreBox.append(el('p', 'tiny muted',
  'Автоустановка требует подключённую USB WINDOWS_10 и свежую успешную проверку VPN. Неудачная версия блокируется для автоматических повторов. За ночь выполняется не больше одной попытки; промежуточные скачанные файлы хранятся на USB и удаляются после операции.'
  ));
const corePassword = field(coreBox, 'Текущий пароль панели', 'password'),
  coreRetry = check(coreBox, 'Явно повторить установку ранее неудачной версии');
button(coreBox, 'Сохранить режим обновления Xray', async () => {
  if (coreAutoEnabled.checked && !confirm(
      'Разрешить автоматическую установку стабильных выпусков и пререлизов в выбранный час МСК? VPN-соединения будут переподключаться.'
      )) return;
  await api('xray-settings', 'POST', {
    password: corePassword.value,
    settings: {
      check: coreCheckEnabled.checked,
      auto_install: coreAutoEnabled.checked,
      hour_msk: Number(coreHour.value)
    }
  });
  corePassword.value = '';
  notice('Режим обновления Xray сохранён.');
  if (coreLoadRequest) await coreLoadRequest.catch(() => {});
  coreDraft.saved();
  await loadCore()
});
const coreInstallButton = button(coreBox, 'Установить проверенный Xray', async () => {
  cleanDraft();
  if (!coreLatest) throw new Error('Сначала проверь обновления Xray.');
  if (!confirm(
      'Проверить архив и конфиг, сохранить прежнее ядро на USB и обновить Xray? VPN-соединения кратко переподключатся.'
      )) return;
  try {
    await api('xray-install', 'POST', {
      password: corePassword.value,
      version: coreLatest,
      retry: coreRetry.checked
    })
  } finally {
    corePassword.value = ''
  }
  await loadCore();
  startCorePolling()
}, 'primary');
const coreRollbackButton = button(coreBox, 'Вернуть предыдущее ядро с USB', async () => {
  cleanDraft();
  if (!confirm('Восстановить предыдущее ядро с USB? Текущие VPN-соединения переподключатся.')) return;
  try {
    await api('xray-rollback', 'POST', {
      password: corePassword.value
    })
  } finally {
    corePassword.value = ''
  }
  await loadCore();
  startCorePolling()
});
// Navigation and job polling share one in-flight read, avoiding duplicate requests.
function loadCore() {
  if (!coreLoadRequest) coreLoadRequest = readCore().finally(() => {
    coreLoadRequest = null
  });
  return coreLoadRequest;
}
async function readCore() {
  const d = await api('xray'),
    s = d.status,
    p = d.settings,
    j = d.job;
  coreLatest = s.available ? s.latest : '';
  coreBusy = !!j.id && !j.finished;
  coreDraft.sync(() => {
    coreCheckEnabled.checked = p.check;
    coreAutoEnabled.checked = p.auto_install;
    coreHour.value = p.hour_msk
  });
  coreInstallButton.disabled = !s.available || coreBusy;
  coreInstallButton.dataset.locked = String(coreInstallButton.disabled);
  coreRollbackButton.disabled = !d.rollback_available || coreBusy;
  coreRollbackButton.dataset.locked = String(coreRollbackButton.disabled);
  facts(coreFacts, [
    ['Установлено', s.current || 'Не определено'],
    ['Последний выпуск', s.latest || 'Ещё не проверен'],
    ['Тип выпуска', s.latest ? (s.prerelease ? 'Пререлиз' : 'Стабильный') : 'Ещё не проверен'],
    ['Проверено', stamp(s.checked_at)],
    ['Состояние', s.error || (s.available ? 'Доступно обновление' : s.latest && s.current && s
      .latest !== s.current ? 'Источник предлагает более старую версию — понижение запрещено' :
      'Обновлений нет')],
    ['Автоустановка', p.auto_install ? 'В ' + p.hour_msk +
      ':00 МСК, при исправном VPN и подключённой USB' : 'Выключена'
    ],
    ['Заблокированные версии', Object.keys(p.blocked_versions || {}).join(', ') || 'Нет']
  ]);
  coreNotes.textContent = s.notes || '';
  coreProgressText.textContent = j.message ? (stamp(j.started) + ' · ' + j.message) :
    'Операций обновления ядра ещё не было.';
  if (coreBusy) startCorePolling()
}

function startCorePolling() {
  if (corePoll) return;
  corePoll = setInterval(async () => {
    if (!csrf) {
      clearInterval(corePoll);
      corePoll = null;
      return
    }
    if (document.hidden || corePollBusy) return;
    corePollBusy = true;
    try {
      await loadCore();
      if (!coreBusy) {
        clearInterval(corePoll);
        corePoll = null;
        await status()
      }
    } catch (e) {
      notice(e.message, true)
    } finally {
      corePollBusy = false
    }
  }, 2500)
}
const cleanupBox = box($('backups-view'), 'Автоочистка через неделю',
  'Удаляем только известные лишние копии и временные файлы старше 7 дней. Последняя копия панели, последняя копия ядра и последний снимок настроек остаются. Текущие программы, VPN-ключи, пароль и USB-автозапуск не удаляются.'
  );
const cleanupEnabled = check(cleanupBox, 'Удалять лишние файлы старше недели'),
  cleanupPassword = field(cleanupBox, 'Текущий пароль панели', 'password'),
  cleanupFacts = output(cleanupBox);
const cleanupDraft = draftFields([cleanupEnabled]);
button(cleanupBox, 'Сохранить автоочистку', async () => {
  await api('cleanup-settings', 'POST', {
    password: cleanupPassword.value,
    enabled: cleanupEnabled.checked
  });
  cleanupPassword.value = '';
  cleanupDraft.saved();
  notice('Настройка автоочистки сохранена.');
  await loadStorage()
});
button(cleanupBox, 'Очистить старше 7 дней сейчас', async () => {
  if (!confirm('Удалить известные лишние файлы старше недели, сохранив последние резервные копии?'))
    return;
  const d = await api('cleanup-now', 'POST', {
    password: cleanupPassword.value
  });
  cleanupPassword.value = '';
  notice('Удалено файлов/каталогов: ' + d.removed + ' · ' + (d.bytes / 1048576).toFixed(2) + ' МиБ');
  await loadStorage();
  await loadBackups()
});
const loadStorageBase = loadStorage;
loadStorage = async function() {
  const d = await loadStorageBase();
  cleanupDraft.sync(() => {
    cleanupEnabled.checked = d.cleanup?.enabled ?? true
  });
  facts(cleanupFacts, [
    ['Порог', String(d.retention_days || 7) + ' дней'],
    ['Последний обход', stamp(d.last_cleanup?.at)],
    ['Удалено за обход', d.last_cleanup?.removed || 0],
    ['Удалено данных', ((d.last_cleanup?.bytes || 0) / 1048576).toFixed(2) + ' МиБ'],
    ['Состояние', d.last_cleanup?.error || 'Обход каждые 6 часов после синхронизации часов']
  ])
};
