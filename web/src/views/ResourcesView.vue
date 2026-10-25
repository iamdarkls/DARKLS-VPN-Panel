<script setup lang="ts">
import { computed, ref } from 'vue';
import { api } from '../api';
import { mib, bytes, stamp } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
import ActionButton from '../components/ActionButton.vue';
defineProps<{ mode: 'metrics' | 'logs' }>();
interface Metrics {
  cpu_percent: number | null;
  load?: string[];
  memory_available_kib?: number;
  memory_total_kib?: number;
  processes?: Record<string, { rss_kib?: number; swap_kib?: number }>;
  jffs_free_bytes?: number;
  jffs_total_bytes?: number;
  uptime_seconds: number;
  usb?: boolean;
}
const metrics = ref<Metrics>(),
  logs = ref<{
    events: { at: number; message: string }[];
    watch: string;
    panel: string;
    update: string;
  }>();
const values = computed(() => {
  const m = metrics.value;
  if (!m) return [];
  return [
    [
      'Процессор',
      m.cpu_percent == null ? 'CPU недоступен' : m.cpu_percent.toFixed(1) + '%',
      'Load: ' + (m.load || []).join(' / '),
    ],
    ['Доступная RAM', mib(m.memory_available_kib), 'Всего: ' + mib(m.memory_total_kib)],
    [
      'Панель · RAM',
      mib(m.processes?.panel?.rss_kib),
      'Swap: ' + mib(m.processes?.panel?.swap_kib),
    ],
    ['Xray · RAM', mib(m.processes?.xray?.rss_kib), 'Swap: ' + mib(m.processes?.xray?.swap_kib)],
    ['Свободно /jffs', bytes(m.jffs_free_bytes), 'Всего: ' + bytes(m.jffs_total_bytes)],
    [
      'Время работы',
      Math.floor(m.uptime_seconds / 3600) +
        ' ч ' +
        Math.floor((m.uptime_seconds % 3600) / 60) +
        ' мин',
      'USB: ' + (m.usb ? 'подключена' : 'не обнаружена'),
    ],
  ];
});
async function refresh(mode?: string) {
  if (mode === 'logs') logs.value = await api('logs');
  else metrics.value = await api('metrics');
}
defineExpose({ refresh });
</script>
<template>
  <section :id="mode + '-view'">
    <template v-if="mode === 'metrics'">
      <div id="metrics-grid" class="metrics-grid">
        <div v-for="[label, value, sub] in values" :key="label" class="metric-card">
          <span class="muted">{{ label }}</span>
          <strong>{{ value }}</strong>
          <span class="tiny muted">{{ sub }}</span>
        </div>
      </div>
      <p class="footnote">
        Память процессов — RSS. CPU — среднее между замерами. Опрос выполняется только при открытой
        вкладке панели.
      </p>
    </template>
    <template v-else>
      <PanelBox title="События">
        <ActionButton id="refresh-logs" :action="() => refresh('logs')">Обновить</ActionButton>
        <div id="event-list">
          <div v-for="(event, i) in logs?.events" :key="i" class="event-row">
            <span class="tiny muted">{{ stamp(event.at) }}</span>
            <strong>{{ event.message }}</strong>
          </div>
          <p v-if="!logs?.events?.length" class="muted">Событий пока нет.</p>
        </div>
      </PanelBox>
      <details
        v-for="[key, title] in [
          ['watch', 'Журнал автоконтроля'],
          ['panel', 'Журнал панели'],
          ['update', 'Журнал обновлений'],
        ]"
        :key="key"
        class="panel-box"
      >
        <summary>{{ title }}</summary>
        <pre :id="key + '-log'">{{
          logs?.[key as 'watch' | 'panel' | 'update'] || 'Записей пока нет.'
        }}</pre>
      </details>
      <p class="footnote">
        Журналы хранятся в RAM с ограничением размера и очищаются после перезагрузки роутера.
      </p>
    </template>
  </section>
</template>
