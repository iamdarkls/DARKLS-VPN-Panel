<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { api } from '../api';
import type { ServiceHistory, Preferences, Profile } from '../types';
import { activity, loadActivity, loadState, refreshStatus, cleanDraft } from '../store';
import { notice, stamp, duration, serviceLabel } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import DataTable from '../components/DataTable.vue';
import ActionButton from '../components/ActionButton.vue';
defineProps<{ tab: string }>();
const history = ref<ServiceHistory>({}),
  password = ref(''),
  xrayPassword = ref(''),
  monitorPassword = ref(''),
  backup = ref(false),
  changed = ref(false),
  form = reactive<Preferences>({
    monitor_services: true,
    interval_seconds: 300,
    auto_failover: false,
  });
const summary = computed(() => {
  const s = activity.value.summary || {};
  return [
    ['Время наблюдения', duration(s.observed_seconds)],
    ['Недоступность в наблюдаемые интервалы', duration(s.unavailable_seconds)],
    ['Нет данных', duration(s.unknown_seconds)],
    ['Эпизоды, начавшиеся за сутки', s.failure_episodes || 0],
    ['Попытки восстановления', s.recoveries || 0],
    ['Последняя проба', !s.probe_fresh ? 'Нет свежей пробы' : s.probe_ok ? 'Успешна' : 'Ошибка'],
    [
      'Пик занятой RAM',
      s.resource_samples ? (Number(s.peak_used_kib) / 1024).toFixed(1) + ' МиБ' : 'Нет замеров',
    ],
    [
      'Пик RSS панели / Xray',
      s.resource_samples
        ? (Number(s.peak_panel_kib) / 1024).toFixed(1) +
          ' / ' +
          (Number(s.peak_xray_kib) / 1024).toFixed(1) +
          ' МиБ'
        : 'Нет замеров',
    ],
  ] as [string, string | number][];
});
const rows = computed(() =>
  Object.values(history.value.current || {}).map((r) => ({
    service: r.service,
    state: serviceLabel(r.state),
    since: stamp(r.since),
    at: stamp(r.at),
    tunnel: r.tunnel_ok == null ? 'Нет свежего замера' : r.tunnel_ok ? 'Работал' : 'Ошибка',
  }))
);
async function loadHistory() {
  history.value = await api<ServiceHistory>('service-history');
}
async function refresh(tab = 'history') {
  if (tab === 'history') {
    await loadActivity(true);
    await loadHistory();
  } else if (tab === 'monitor') {
    const [p, profiles] = await Promise.all([
      api<Preferences>('preferences'),
      api<Profile>('profiles'),
    ]);
    backup.value = !!profiles.backup_configured;
    if (!changed.value) Object.assign(form, p);
  }
}
async function recoverPending() {
  try {
    await api('recovery', 'POST', { password: password.value });
    notice('Восстановление завершено');
    await loadState();
    await refreshStatus();
  } finally {
    password.value = '';
  }
}
async function repair(component: string) {
  cleanDraft();
  if (component === 'xray' && !confirm('Текущие соединения через VPN переподключатся. Продолжить?'))
    return;
  try {
    await api('recover-part', 'POST', {
      component,
      ...(component === 'xray' ? { password: xrayPassword.value } : {}),
    });
    notice('Проверка / восстановление выполнены. Подробности в журнале.');
    await refreshStatus();
  } finally {
    xrayPassword.value = '';
  }
}
async function save() {
  try {
    await api('preferences', 'POST', { password: monitorPassword.value, preferences: { ...form } });
    changed.value = false;
    await refresh('monitor');
    notice('Настройки контроля сохранены.');
  } finally {
    monitorPassword.value = '';
  }
}
defineExpose({ refresh });
</script>
<template>
  <section id="stability-view">
    <div
      id="stability-history"
      v-show="tab === 'history'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-history"
      tabindex="0"
    >
      <PanelBox
        title="Сводка за последние 24 часа"
        description="Доступность по HTTPS-пробам общего туннеля. Состояние отдельных сайтов может отличаться."
      >
        <Facts :rows="summary" />
        <p class="tiny muted">{{ activity.summary?.note }}</p>
      </PanelBox>
      <PanelBox
        title="История состояния сервисов"
        description="Ошибка DNS, тайм-аут, ответ сайта и Cloudflare показаны отдельно."
      >
        <ActionButton :action="loadHistory">Обновить историю</ActionButton>
        <DataTable
          :rows="rows"
          :columns="[
            ['service', 'Сервис'],
            ['state', 'Последний результат'],
            ['since', 'Состояние с'],
            ['at', 'Проверено'],
            ['tunnel', 'Общий туннель'],
          ]"
        />
        <details>
          <summary>Последние изменения и длительность</summary>
          <p
            v-for="(r, i) in (history.events || [])
              .filter((r) => r.previous)
              .slice(-30)
              .reverse()"
            :key="i"
            class="tiny"
          >
            {{ stamp(r.at) }} · {{ r.service }}: {{ serviceLabel(r.previous || '') }} →
            {{ serviceLabel(r.state) }} · прежнее состояние {{ r.previous_duration_seconds }} с
          </p>
        </details>
        <p class="tiny muted">{{ history.retention }}</p>
      </PanelBox>
      <PanelBox
        title="Уведомления о связи"
        description="События за сутки. Отметка прочитанного хранится только в этом браузере."
      >
        <p v-for="item in activity.notifications" :key="item.id">
          {{ stamp(item.at) }} · {{ item.message }}
        </p>
        <p v-if="!activity.notifications?.length" class="muted">Событий за сутки нет.</p>
      </PanelBox>
    </div>
    <div
      id="stability-recovery"
      v-show="tab === 'recovery'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-recovery"
      tabindex="0"
    >
      <PanelBox
        title="Завершить незавершённую операцию"
        description="Подключи USB с резервной копией и повтори откат. Изменения доступны после успешного восстановления."
      >
        <label>
          Пароль для завершения восстановления
          <input v-model="password" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="recoverPending">Повторить восстановление</ActionButton>
      </PanelBox>
      <PanelBox
        title="Восстановить отдельную часть"
        description="Автоконтроль сначала чинит процессы, DNS helper и правила. Xray перезапускается при подтверждённой неисправности туннеля или DNS-входа."
      >
        <ActionButton
          v-for="[component, label] in [
            ['auto', 'Найти и восстановить'],
            ['dns', 'Только DNS helper'],
            ['rules', 'Только правила'],
          ]"
          :key="component"
          :action="() => repair(component!)"
        >
          {{ label }}
        </ActionButton>
        <label>
          Пароль для ручного перезапуска Xray
          <input v-model="xrayPassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="() => repair('xray')">Перезапустить Xray</ActionButton>
      </PanelBox>
    </div>
    <div
      id="stability-monitor"
      v-show="tab === 'monitor'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-stability-monitor"
      tabindex="0"
    >
      <PanelBox
        title="Контроль связи"
        class="wide"
        description="Проверки работают независимо от открытой панели. Отказ одного сервиса не перезапускает Xray. Резерв включается при подтверждённом отказе туннеля; автоматического возврата на основной нет."
      >
        <div @input="changed = true" @change="changed = true">
          <label class="quic-label">
            <input v-model="form.monitor_services" type="checkbox" />
            Периодически проверять включённые сервисы
          </label>
          <label>
            Интервал в секундах: 120–3600
            <input v-model.number="form.interval_seconds" type="number" min="120" max="3600" />
          </label>
          <label class="quic-label">
            <input v-model="form.auto_failover" type="checkbox" :disabled="!backup" />
            Автоматически переключаться на проверенный резерв
          </label>
        </div>
        <label>
          Текущий пароль
          <input v-model="monitorPassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="save">Сохранить контроль связи</ActionButton>
      </PanelBox>
    </div>
  </section>
</template>
