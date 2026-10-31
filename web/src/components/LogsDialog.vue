<script setup lang="ts">
import Modal from './Modal.vue';

import { useLogsDialog } from '../composables/useLogsDialog';
const {
  source,
  sources,
  limit,
  level,
  auto,
  pending,
  run,
  refresh,
  lines,
  download,
  search,
  severity,
} = useLogsDialog();
defineEmits<{ close: [] }>();
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
