<script setup lang="ts">
import { ref } from 'vue';
import { api } from '../api';
import type { Storage, PanelState } from '../types';
import { draft, dirty, busy, revision, loadState, refreshStatus } from '../store';
import { bytes, stamp, notice, useAction } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
import Facts from '../components/Facts.vue';
import ActionButton from '../components/ActionButton.vue';
defineProps<{ tab: string }>();
const backups = ref<string[]>([]),
  storage = ref<Storage>({}),
  enabled = ref(true),
  cleanupChanged = ref(false),
  cleanupPassword = ref(''),
  deletePassword = ref(''),
  file = ref<File>();
const { pending, run } = useAction();
async function loadBackups() {
  backups.value = (await api<{ backups: string[] }>('backups')).backups;
}
async function loadStorage() {
  storage.value = await api<Storage>('storage');
  if (!cleanupChanged.value) enabled.value = storage.value.cleanup?.enabled ?? true;
}
async function restore(id: string) {
  if (dirty.value && !confirm('Есть несохранённые изменения. Продолжить?')) return;
  if (!confirm('Восстановить копию ' + id + '?')) return;
  busy.value = true;
  try {
    await api('restore', 'POST', { backup: id, revision: revision.value });
    await loadState();
    await loadBackups();
    notice(
      'Резервная копия восстановлена. Прежние IP удалённых доменов могут сохраняться до 24 часов.'
    );
  } finally {
    busy.value = false;
    await refreshStatus();
  }
}
async function importFile() {
  if (!file.value || file.value.size > 512 * 1024)
    throw new Error('Выбери JSON размером до 512 КиБ.');
  if (dirty.value && !confirm('Импорт заменит несохранённые изменения. Продолжить?')) return;
  let input: unknown;
  try {
    input = JSON.parse(await file.value.text());
  } catch {
    throw new Error('Файл не является корректным JSON.');
  }
  const d = await api<{ state: PanelState; groups: number; domains: number }>(
    'import-preview',
    'POST',
    input
  );
  draft.value = d.state;
  notice(
    'Импорт просмотрен: ' +
      d.groups +
      ' групп, ' +
      d.domains +
      ' активных доменов. Нажми «Применить изменения», чтобы сохранить.'
  );
}
async function saveCleanup() {
  try {
    await api('cleanup-settings', 'POST', {
      password: cleanupPassword.value,
      enabled: enabled.value,
    });
    cleanupChanged.value = false;
    await loadStorage();
    notice('Настройка автоочистки сохранена.');
  } finally {
    cleanupPassword.value = '';
  }
}
async function cleanup() {
  if (!confirm('Удалить известные лишние файлы старше недели, сохранив последние резервные копии?'))
    return;
  try {
    const d = await api<{ removed: number; bytes: number }>('cleanup-now', 'POST', {
      password: cleanupPassword.value,
    });
    notice('Удалено файлов/каталогов: ' + d.removed + ' · ' + bytes(d.bytes));
    await loadStorage();
    await loadBackups();
  } finally {
    cleanupPassword.value = '';
  }
}
async function remove(item: { id: string; name: string; consequence: string }) {
  if (!confirm(item.consequence + ' Удалить «' + item.name + '»?')) return;
  try {
    await api('storage-delete', 'POST', {
      id: item.id,
      password: deletePassword.value,
      acknowledge: true,
    });
    await loadStorage();
    await loadBackups();
    notice('Выбранная копия удалена.');
  } finally {
    deletePassword.value = '';
  }
}
async function refresh(tab = 'history') {
  if (tab === 'history') await loadBackups();
  else if (tab === 'files') await loadStorage();
}
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
