'use strict';
// The controller owns one in-flight read and one timer per Xray update job.
class XrayUpdatesController {
  constructor() {
    this.box = box($('updates-core'), 'Обновления ядра Xray',
      'Официальные стабильные выпуски и пререлизы XTLS/Xray-core для Linux ARM64. Автопроверка раз в сутки; установка по кнопке или в выбранный час МСК. Автоустановка изначально выключена. При замене ядра VPN-соединения кратко переподключатся.'
      );
    this.box.classList.add('wide');
    this.facts = output(this.box),
      this.notes = el('pre', 'log-box'),
      this.progress = el('p', 'tiny muted');
    this.progress.setAttribute('role', 'status');
    this.box.append(this.notes, this.progress);
    this.latest = '',
      this.busy = false,
      this.timer = null,
      this.pollBusy = false,
      this.request = null;
    button(this.box, 'Проверить обновления Xray', async () => {
      const st = await api('xray-check', 'POST', {});
      if (st.error) notice(st.error, true);
      await this.load()
    });
    this.checkEnabled = check(this.box, 'Автоматически проверять обновления Xray'),
      this.autoEnabled = check(this.box, 'Устанавливать обновления, включая пререлизы, автоматически'),
      this.hour = field(this.box, 'Час автоустановки, московское время (0–23)', 'number');
    this.hour.min = 0;
    this.hour.max = 23;
    this.delay = field(this.box, 'Отсрочка после публикации, дней (0–30)', 'number');
    this.delay.min = 0; this.delay.max = 30;
    this.draft = draftFields([this.checkEnabled, this.autoEnabled, this.hour, this.delay]);
    this.box.append(el('p', 'tiny muted',
      'Отсрочка действует на стабильные выпуски и пререлизы; ручная установка доступна сразу. Если дата публикации неизвестна, автоустановка с отсрочкой ждёт следующей проверки. Автоустановка требует подключённую USB WINDOWS_10 и свежую успешную проверку VPN. Неудачная версия блокируется для автоматических повторов. За ночь выполняется не больше одной попытки; промежуточные скачанные файлы хранятся на USB и удаляются после операции.'
      ));
    this.password = field(this.box, 'Текущий пароль панели', 'password'),
      this.retry = check(this.box, 'Явно повторить установку ранее неудачной версии');
    button(this.box, 'Сохранить режим обновления Xray', async () => {
      if (this.autoEnabled.checked && !confirm(
          'Разрешить автоматическую установку стабильных выпусков и пререлизов в выбранный час МСК? VPN-соединения будут переподключаться.'
          )) return;
      await api('xray-settings', 'POST', {
        password: this.password.value,
        settings: {
          check: this.checkEnabled.checked,
          auto_install: this.autoEnabled.checked,
          hour_msk: Number(this.hour.value),
          delay_days: Number(this.delay.value)
        }
      });
      this.password.value = '';
      notice('Режим обновления Xray сохранён.');
      if (this.request) await this.request.catch(() => {});
      this.draft.saved();
      await this.load()
    });
    this.installButton = button(this.box, 'Установить проверенный Xray', async () => {
      cleanDraft();
      if (!this.latest) throw new Error('Сначала проверь обновления Xray.');
      if (!confirm(
          'Проверить архив и конфиг, сохранить прежнее ядро на USB и обновить Xray? VPN-соединения кратко переподключатся.'
          )) return;
      try {
        await api('xray-install', 'POST', {
          password: this.password.value,
          version: this.latest,
          retry: this.retry.checked
        })
      } finally {
        this.password.value = ''
      }
      await this.load();
      this.startPolling()
    }, 'primary');
    this.rollbackButton = button(this.box, 'Вернуть предыдущее ядро с USB', async () => {
      cleanDraft();
      if (!confirm('Восстановить предыдущее ядро с USB? Текущие VPN-соединения переподключатся.')) return;
      try {
        await api('xray-rollback', 'POST', {
          password: this.password.value
        })
      } finally {
        this.password.value = ''
      }
      await this.load();
      this.startPolling()
    });
  }
  // Navigation and job polling share one in-flight read, avoiding duplicate requests.
  load() {
    if (!this.request) this.request = this.read().finally(() => {
      this.request = null
    });
    return this.request;
  }
  async read() {
    const d = await api('xray'),
      s = d.status,
      p = d.settings,
      j = d.job;
    this.latest = s.available ? s.latest : '';
    this.busy = !!j.id && !j.finished;
    this.draft.sync(() => {
      this.checkEnabled.checked = p.check;
      this.autoEnabled.checked = p.auto_install;
      this.hour.value = p.hour_msk;
      this.delay.value = p.delay_days ?? 3
    });
    this.installButton.disabled = !s.available || this.busy;
    this.installButton.dataset.locked = String(this.installButton.disabled);
    this.rollbackButton.disabled = !d.rollback_available || this.busy;
    this.rollbackButton.dataset.locked = String(this.rollbackButton.disabled);
    facts(this.facts, [
      ['Установлено', s.current || 'Не определено'],
      ['Последний выпуск', s.latest || 'Ещё не проверен'],
      ['Тип выпуска', s.latest ? (s.prerelease ? 'Пререлиз' : 'Стабильный') : 'Ещё не проверен'],
      ['Проверено', stamp(s.checked_at)],
      ['Опубликован', stamp(s.published_at)],
      ['Автоустановка не раньше', (p.delay_days ?? 3) === 0 ? 'Без отсрочки' : s.published_at ? stamp(s.published_at + (p.delay_days ?? 3) * 86400) : 'Ожидаем дату публикации'],
      ['Состояние', s.error || (s.available ? 'Доступно обновление' : s.latest && s.current && s
        .latest !== s.current ? 'Источник предлагает более старую версию — понижение запрещено' :
        'Обновлений нет')],
      ['Автоустановка', p.auto_install ? 'В ' + p.hour_msk +
        ':00 МСК, при исправном VPN и подключённой USB' : 'Выключена'
      ],
      ['Заблокированные версии', Object.keys(p.blocked_versions || {}).join(', ') || 'Нет']
    ]);
    this.notes.textContent = s.notes || '';
    this.progress.textContent = j.message ? (stamp(j.started) + ' · ' + j.message) :
      'Операций обновления ядра ещё не было.';
    if (this.busy) this.startPolling()
  }

  startPolling() {
    if (this.timer) return;
    this.timer = setInterval(async () => {
      if (!csrf) {
        clearInterval(this.timer);
        this.timer = null;
        return
      }
      if (document.hidden || this.pollBusy) return;
      this.pollBusy = true;
      try {
        await this.load();
        if (!this.busy) {
          clearInterval(this.timer);
          this.timer = null;
          await status()
        }
      } catch (e) {
        notice(e.message, true)
      } finally {
        this.pollBusy = false
      }
    }, 2500)
  }
}
const xrayUpdates = new XrayUpdatesController();
function loadCore() { return xrayUpdates.load() }

const cleanupBox = box($('backups-files'), 'Автоочистка через неделю',
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
