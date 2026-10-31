<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';

import { useBackups } from '../composables/useBackups';
const {
  backups,
  busy,
  restore,
  run,
  importFile,
  file,
  pending,
  deletePassword,
  loadStorage,
  storage,
  bytes,
  remove,
  enabled,
  cleanupChanged,
  cleanupPassword,
  stamp,
  saveCleanup,
  cleanup,
  refresh,
} = useBackups();
defineProps<{ tab: string }>();
defineExpose({ refresh });
</script>
<template>
  <section id="backups-view">
    <div
      id="backups-history"
      v-show="tab === 'history'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-backups-history"
      tabindex="0"
    >
      <div class="section-label">
        <h2>История настроек</h2>
        <a class="secondary download" href="/api/export">Скачать текущий список ↓</a>
      </div>
      <p class="muted">Копия создаётся перед каждым применением. Храним последние 12.</p>
      <div id="backups" class="backup-list">
        <div v-for="id in backups" :key="id" class="backup-row">
          <div>
            <strong>
              {{ id.slice(0, 8) }} {{ id.slice(9, 11) }}:{{ id.slice(11, 13) }}:{{
                id.slice(13, 15)
              }}
            </strong>
            <span class="tiny">{{ id }}</span>
          </div>
          <ActionButton class="restore-button" :disabled="busy" :action="() => restore(id)">
            Восстановить
          </ActionButton>
        </div>
        <div v-if="!backups.length" class="empty">
          Копии появятся после первого применения настроек.
        </div>
      </div>
    </div>
    <div
      id="backups-transfer"
      v-show="tab === 'transfer'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-backups-transfer"
      tabindex="0"
    >
      <PanelBox
        title="Перенести настройки"
        description="Группы, разделы и выбор устройств. Пароль и параметры подключения в экспорт не входят."
      >
        <a href="/api/export" class="secondary download">Скачать настройки ↓</a>
        <form id="import-form" @submit.prevent="run(importFile)">
          <label>
            Загрузить JSON
            <input
              id="import-file"
              type="file"
              accept=".json,application/json"
              required
              @change="file = ($event.target as HTMLInputElement).files?.[0]"
            />
          </label>
          <button class="secondary" type="submit" :disabled="pending">Просмотреть импорт</button>
        </form>
        <p class="tiny muted">
          После просмотра нажми «Применить изменения», чтобы сохранить импорт.
        </p>
      </PanelBox>
    </div>
    <div
      id="backups-files"
      v-show="tab === 'files'"
      class="tab-panel"
      role="tabpanel"
      aria-labelledby="tab-backups-files"
      tabindex="0"
    >
      <PanelBox
        title="Копии и установочные архивы"
        description="Доступные известные файлы отдельно от работающей панели и Xray. Текущие программы, настройки и USB-автозапуск удалить здесь нельзя."
      >
        <label>
          Пароль для удаления выбранной копии
          <input v-model="deletePassword" type="password" autocomplete="current-password" />
        </label>
        <ActionButton :action="loadStorage">Обновить список файлов</ActionButton>
        <div v-for="item in storage.items" :key="item.id" class="panel-box">
          <strong>{{ item.name }} · {{ bytes(item.bytes) }}</strong>
          <p class="tiny muted">{{ item.consequence }}</p>
          <ActionButton kind="danger" :action="() => remove(item)">Удалить эту копию</ActionButton>
        </div>
        <p v-if="!storage.items?.length" class="muted">Копии и известные архивы не найдены.</p>
      </PanelBox>
      <PanelBox
        title="Автоочистка через неделю"
        description="Удаляем только известные лишние копии и временные файлы старше 7 дней. Последние копии панели, ядра и настроек сохраняются."
      >
        <label class="quic-label">
          <input v-model="enabled" type="checkbox" @change="cleanupChanged = true" />
          Удалять лишние файлы старше недели
        </label>
        <label>
          Текущий пароль панели
          <input v-model="cleanupPassword" type="password" autocomplete="current-password" />
        </label>
        <Facts
          :rows="[
            ['Порог', (storage.retention_days || 7) + ' дней'],
            ['Последний обход', stamp(storage.last_cleanup?.at)],
            ['Удалено за обход', storage.last_cleanup?.removed || 0],
            ['Удалено данных', bytes(storage.last_cleanup?.bytes)],
            [
              'Состояние',
              storage.last_cleanup?.error || 'Обход каждые 6 часов после синхронизации часов',
            ],
          ]"
        />
        <ActionButton :action="saveCleanup">Сохранить автоочистку</ActionButton>
        <ActionButton :action="cleanup">Очистить старше 7 дней сейчас</ActionButton>
      </PanelBox>
    </div>
  </section>
</template>
