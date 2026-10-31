import { registerUnsaved } from '../stores/forms';
import { ref } from 'vue';
import { api } from '../api';
import type { Storage, PanelState } from '../types';
import { draft, dirty, revision, loadState } from '../stores/draft';
import { busy } from '../stores/sessionState';
import { refreshStatus } from '../stores/vpn';
import { bytes, stamp, notice, useAction } from './ui';

/** Settings snapshots, import preview and bounded storage cleanup; refresh only the selected tab.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useBackups() {
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
    if (
      !confirm('Удалить известные лишние файлы старше недели, сохранив последние резервные копии?')
    )
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
  registerUnsaved(() => cleanupChanged.value);
  return {
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
  };
}
