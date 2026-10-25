<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { api } from '../api';
import type { CoreResponse, CoreSettings } from '../types';
import { cleanDraft, refreshStatus } from '../store';
import { notice, stamp, usePolling } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';
const data = ref<CoreResponse>({
    status: {},
    settings: { check: true, auto_install: false, hour_msk: 4, delay_days: 3 },
    job: {},
  }),
  form = reactive<CoreSettings>({ check: true, auto_install: false, hour_msk: 4, delay_days: 3 }),
  changed = ref(false),
  password = ref(''),
  retry = ref(false);
let request: Promise<void> | undefined;
const working = computed(() => !!data.value.job.id && !data.value.job.finished),
  latest = computed(() => (data.value.status.available ? data.value.status.latest : ''));
function load() {
  if (!request)
    request = (async () => {
      const d = await api<CoreResponse>('xray');
      data.value = d;
      if (!changed.value)
        Object.assign(form, {
          check: d.settings.check,
          auto_install: d.settings.auto_install,
          hour_msk: d.settings.hour_msk,
          delay_days: d.settings.delay_days ?? 3,
        });
    })().finally(() => {
      request = undefined;
    });
  return request;
}
const facts = computed(() => {
  const { status: s, settings: p } = data.value;
  return [
    ['Установлено', s.current || 'Не определено'],
    ['Последний выпуск', s.latest || 'Ещё не проверен'],
    ['Тип выпуска', s.latest ? (s.prerelease ? 'Пререлиз' : 'Стабильный') : 'Ещё не проверен'],
    ['Проверено', stamp(s.checked_at)],
    ['Опубликован', stamp(s.published_at)],
    [
      'Автоустановка не раньше',
      (p.delay_days ?? 3) === 0
        ? 'Без отсрочки'
        : s.published_at
          ? stamp(s.published_at + (p.delay_days ?? 3) * 86400)
          : 'Ожидаем дату публикации',
    ],
    [
      'Состояние',
      s.error ||
        (s.available
          ? 'Доступно обновление'
          : s.latest && s.current && s.latest !== s.current
            ? 'Источник предлагает более старую версию — понижение запрещено'
            : 'Обновлений нет'),
    ],
    [
      'Автоустановка',
      p.auto_install
        ? 'В ' + p.hour_msk + ':00 МСК, при исправном VPN и подключённой USB'
        : 'Выключена',
    ],
    ['Заблокированные версии', Object.keys(p.blocked_versions || {}).join(', ') || 'Нет'],
  ] as [string, string][];
});
async function save() {
  if (
    form.auto_install &&
    !confirm(
      'Разрешить автоматическую установку стабильных выпусков и пререлизов в выбранный час МСК? VPN-соединения будут переподключаться.'
    )
  )
    return;
  try {
    await api('xray-settings', 'POST', { password: password.value, settings: { ...form } });
    if (request) await request.catch(() => {});
    changed.value = false;
    await load();
    notice('Режим обновления Xray сохранён.');
  } finally {
    password.value = '';
  }
}
async function install() {
  cleanDraft();
  const version = latest.value;
  if (!version) throw new Error('Сначала проверь обновления Xray.');
  if (
    !confirm(
      'Проверить архив и конфиг, сохранить прежнее ядро на USB и обновить Xray? VPN-соединения кратко переподключатся.'
    )
  )
    return;
  try {
    await api('xray-install', 'POST', { password: password.value, version, retry: retry.value });
    await load();
  } finally {
    password.value = '';
  }
}
async function rollback() {
  cleanDraft();
  if (!confirm('Восстановить предыдущее ядро с USB? Текущие VPN-соединения переподключатся.'))
    return;
  try {
    await api('xray-rollback', 'POST', { password: password.value });
    await load();
  } finally {
    password.value = '';
  }
}
usePolling(async () => {
  if (working.value) {
    await load();
    if (!working.value) await refreshStatus();
  }
}, 2500);
defineExpose({ refresh: load });
</script>
<template>
  <PanelBox
    title="Обновления ядра Xray"
    class="wide"
    description="Официальные стабильные выпуски и пререлизы XTLS/Xray-core для Linux ARM64. Автоустановка изначально выключена. При замене ядра VPN-соединения кратко переподключатся."
  >
    <Facts :rows="facts" />
    <ActionButton :action="load">Обновить сведения Xray</ActionButton>
    <pre class="log-box">{{ data.status.notes || '' }}</pre>
    <p class="tiny muted" role="status">
      {{
        data.job.message
          ? stamp(data.job.started) + ' · ' + data.job.message
          : 'Операций обновления ядра ещё не было.'
      }}
    </p>
    <ActionButton
      :action="
        async () => {
          const s = await api<{ error?: string }>('xray-check', 'POST', {});
          if (s.error) notice(s.error, true);
          await load();
        }
      "
    >
      Проверить обновления Xray
    </ActionButton>
    <div @input="changed = true" @change="changed = true">
      <label class="quic-label">
        <input v-model="form.check" type="checkbox" />
        Автоматически проверять обновления Xray
      </label>
      <label class="quic-label">
        <input v-model="form.auto_install" type="checkbox" />
        Устанавливать обновления, включая пререлизы, автоматически
      </label>
      <label>
        Час автоустановки, московское время (0–23)
        <input v-model.number="form.hour_msk" type="number" min="0" max="23" />
      </label>
      <label>
        Отсрочка после публикации, дней (0–30)
        <input v-model.number="form.delay_days" type="number" min="0" max="30" />
      </label>
    </div>
    <p class="tiny muted">
      Автоустановка требует USB WINDOWS_10 и свежую успешную проверку VPN. Неудачная версия
      блокируется для автоматических повторов; за ночь — не больше одной попытки. Скачанные файлы
      находятся на USB и удаляются после операции.
    </p>
    <label>
      Текущий пароль панели
      <input v-model="password" type="password" autocomplete="current-password" />
    </label>
    <label class="quic-label">
      <input v-model="retry" type="checkbox" />
      Явно повторить установку ранее неудачной версии
    </label>
    <ActionButton :action="save">Сохранить режим обновления Xray</ActionButton>
    <ActionButton kind="primary" :disabled="!latest || working" :action="install">
      Установить проверенный Xray
    </ActionButton>
    <ActionButton :disabled="!data.rollback_available || working" :action="rollback">
      Вернуть предыдущее ядро с USB
    </ActionButton>
  </PanelBox>
</template>
