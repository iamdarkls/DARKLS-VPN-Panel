<script setup lang="ts">
import { computed, ref, onMounted } from 'vue';
import { api } from '../api';
import { stamp, useAction, usePolling } from '../composables/ui';
import Modal from './Modal.vue';
defineEmits<{ close: [] }>();
interface Logs {
  events: { at: number; message: string }[];
  watch: string;
  panel: string;
  update: string;
}
const data = ref<Logs>(),
  source = ref('events'),
  search = ref(''),
  level = ref('all'),
  limit = ref(100),
  auto = ref(false);
const { pending, run } = useAction();
const sources = [
  ['events', 'События'],
  ['watch', 'Автоконтроль'],
  ['panel', 'Панель'],
  ['update', 'Обновления'],
];
let request: Promise<void> | undefined;
function refresh() {
  return (request ||= api<Logs>('logs')
    .then((value) => {
      data.value = value;
    })
    .finally(() => {
      request = undefined;
    }));
}
function severity(line: string) {
  if (/error|failed|failure|timeout|ошиб|отказ|не отвеч|сбой/i.test(line)) return 'error';
  if (/warn|предупреж/i.test(line)) return 'warning';
  return 'info';
}
const lines = computed(() => {
  const d = data.value;
  const all =
    source.value === 'events'
      ? [...(d?.events || [])]
          .sort((a, b) => b.at - a.at)
          .map((e) => stamp(e.at) + ' · ' + e.message)
      : (d?.[source.value as 'watch' | 'panel' | 'update'] || '').split('\n').filter(Boolean);
  const filtered = all.filter(
    (line) =>
      (!search.value || line.toLowerCase().includes(search.value.toLowerCase())) &&
      (level.value === 'all' || severity(line) === level.value)
  );
  return source.value === 'events' ? filtered.slice(0, limit.value) : filtered.slice(-limit.value);
});
function download() {
  const blob = new Blob([lines.value.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob),
    link = document.createElement('a');
  link.href = url;
  link.download = 'darkls-' + source.value + '.log';
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
onMounted(() => {
  void run(refresh);
});
usePolling(async () => {
  if (auto.value) await refresh();
}, 5000);
</script>
<template>
  <Modal :open="true" label="Журналы" class="logs-dialog" @close="$emit('close')">
    <div class="dialog-heading">
      <div>
        <h2>Журналы</h2>
        <p class="muted tiny">События и диагностика роутера</p>
      </div>
      <button class="icon-button" aria-label="Закрыть журналы" @click="$emit('close')">×</button>
    </div>
    <div class="logs-toolbar">
      <label>
        Источник
        <select v-model="source" aria-label="Источник журнала">
          <option v-for="[id, title] in sources" :key="id" :value="id">{{ title }}</option>
        </select>
      </label>
      <label>
        Строк
        <select v-model.number="limit" aria-label="Количество строк">
          <option :value="20">20</option>
          <option :value="100">100</option>
          <option :value="200">200</option>
        </select>
      </label>
      <label>
        Уровень
        <select v-model="level" aria-label="Уровень записей">
          <option value="all">Все</option>
          <option value="error">Ошибки</option>
          <option value="warning">Предупреждения</option>
          <option value="info">Информация</option>
        </select>
      </label>
      <label class="logs-auto">
        <input v-model="auto" type="checkbox" />
        Автообновление
      </label>
      <button id="refresh-logs" class="secondary" :disabled="pending" @click="run(refresh)">
        Обновить
      </button>
      <button class="primary" :disabled="!lines.length" @click="download">Скачать</button>
    </div>
    <input
      v-model="search"
      class="log-search"
      placeholder="Поиск в журнале…"
      aria-label="Поиск в журнале"
    />
    <div
      id="event-list"
      class="log-console"
      role="region"
      aria-label="Записи журнала"
      tabindex="0"
      :aria-busy="pending"
    >
      <div
        v-for="(line, i) in lines"
        :key="i"
        class="log-line"
        :class="['log-' + severity(line), { 'event-row': source === 'events' }]"
      >
        <span class="log-level">{{ severity(line).toUpperCase() }}</span>
        <span>{{ line }}</span>
      </div>
      <p v-if="!lines.length" class="muted">
        {{ pending ? 'Загружаем журнал…' : 'Подходящих записей нет.' }}
      </p>
    </div>
    <p class="tiny muted log-caption">
      Показано {{ lines.length }} ·
      {{ source === 'events' ? 'Новые события сверху' : 'Исходный порядок строк' }} · Журналы
      ограничены по размеру и хранятся в RAM. Автообновление работает только пока это окно открыто и
      вкладка видна.
    </p>
  </Modal>
</template>
