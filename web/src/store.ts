import { computed, reactive, ref } from 'vue';
import { api, onExpired, setSession, sessionGeneration } from './api';
import type { PanelState, Status, Device, Preview, Activity, Job } from './types';
import { clone, notice, clearNotifications, stamp } from './composables/ui';
export const auth = reactive({
  ready: false,
  loggedIn: false,
  version: '2.0.0',
  error: '',
  epoch: 0,
});
export const draft = ref<PanelState>(),
  original = ref<PanelState>(),
  revision = ref(''),
  catalogNotes = ref<Record<string, string>>({});
export const status = ref<Status>({}),
  devices = ref<Device[]>([]),
  activity = ref<Activity>({}),
  busy = ref(false);
export const dirty = computed(
  () => !!draft.value && JSON.stringify(draft.value) !== JSON.stringify(original.value)
);
export const preview = ref<{ data: Preview; state: PanelState; revision: string }>();
let statusRequest: Promise<void> | undefined,
  activityRequest: Promise<void> | undefined,
  activityAt = 0;
const shown = new Set<string>();
let read = '';
try {
  read = localStorage.getItem('darkls-notifications-read') || '';
} catch {}
export function logoutLocal() {
  setSession('');
  auth.epoch++;
  auth.loggedIn = false;
  draft.value = undefined;
  original.value = undefined;
  revision.value = '';
  preview.value = undefined;
  status.value = {};
  devices.value = [];
  activity.value = {};
  activityAt = 0;
  shown.clear();
  statusRequest = undefined;
  activityRequest = undefined;
  busy.value = false;
  clearNotifications();
}
onExpired(logoutLocal);
export async function loadState() {
  const d = await api<{
    state: PanelState;
    revision: string;
    catalog_notes?: Record<string, string>;
  }>('state');
  d.state.policy ||= { mode: 'all', devices: [] };
  draft.value = d.state;
  original.value = clone(d.state);
  revision.value = d.revision;
  catalogNotes.value = d.catalog_notes || {};
}
export async function openSession() {
  const d = await api<{ csrf: string; version: string }>('session');
  setSession(d.csrf);
  auth.epoch++;
  auth.version = d.version;
  auth.loggedIn = true;
  await loadState();
  await refreshStatus();
}
export async function initialize() {
  try {
    const message = localStorage.getItem('darkls-update-message');
    if (message) {
      auth.error = message;
      localStorage.removeItem('darkls-update-message');
    }
  } catch {}
  try {
    await openSession();
  } catch {
    logoutLocal();
  } finally {
    auth.ready = true;
  }
}
export async function login(user: string, password: string) {
  await api('login', 'POST', { user, password });
  auth.error = '';
  await openSession();
}
export function cleanDraft() {
  if (dirty.value) throw new Error('Сначала примени или отмени изменения списков.');
}
export function discard() {
  if (original.value) draft.value = clone(original.value);
}
export async function loadDevices() {
  devices.value = (await api<{ devices: Device[] }>('devices')).devices || [];
}
export function refreshStatus() {
  if (!auth.loggedIn || busy.value) return Promise.resolve();
  if (statusRequest) return statusRequest;
  const epoch = sessionGeneration();
  const p = (async () => {
    status.value = await api<Status>('status');
    await loadActivity();
  })();
  statusRequest = p.finally(() => {
    if (epoch === sessionGeneration()) statusRequest = undefined;
  });
  return statusRequest;
}
export function loadActivity(force = false) {
  if (!auth.loggedIn || document.hidden || (!force && Date.now() - activityAt < 60000))
    return Promise.resolve();
  if (activityRequest) return activityRequest;
  const epoch = sessionGeneration();
  const p = (async () => {
    const d = await api<Activity>('activity');
    activity.value = d;
    activityAt = Date.now();
    const item = d.notifications?.[0];
    if (item && item.id !== read && !shown.has(item.id)) {
      shown.add(item.id);
      if (shown.size > 100) shown.delete(shown.values().next().value!);
      notice(item.message, item.kind === 'vpn_error' || item.kind === 'update_error', {
        id: 'vpn-' + item.id,
        title: 'Состояние VPN',
        persistent: true,
        onClose: () => {
          read = item.id;
          try {
            localStorage.setItem('darkls-notifications-read', read);
          } catch {}
        },
      });
    }
  })();
  activityRequest = p.finally(() => {
    if (epoch === sessionGeneration()) activityRequest = undefined;
  });
  return activityRequest;
}
export async function prepareApply() {
  if (!draft.value) return;
  busy.value = true;
  try {
    const captured = clone(draft.value),
      expected = revision.value;
    const data = await api<Preview>('preview', 'POST', { state: captured, revision: expected });
    preview.value = { data, state: captured, revision: expected };
  } catch (e) {
    busy.value = false;
    throw e;
  }
}
export function cancelApply() {
  preview.value = undefined;
  busy.value = false;
}
export async function apply() {
  const p = preview.value;
  if (!p) return;
  preview.value = undefined;
  try {
    await api('apply', 'POST', { state: p.state, revision: p.revision });
    await loadState();
    notice(
      'Изменения применены без перезапуска Xray. Для старого DNS-кеша перезапусти приложение или выполни ipconfig /flushdns на ПК.'
    );
  } finally {
    busy.value = false;
    await refreshStatus();
  }
}
export async function temporaryGroup(group: string, minutes: number, quic?: boolean) {
  cleanDraft();
  await api('timer', 'POST', {
    group,
    minutes,
    revision: revision.value,
    ...(quic === undefined ? {} : { quic }),
  });
  await loadState();
  notice(
    'Таймер сохранён. После срока вернётся прежнее состояние; роутер должен синхронизировать часы.'
  );
}
export async function setPower() {
  const enabled = !!status.value.disabled;
  if (!enabled && !confirm('Выключить VPN для домашней сети?')) return;
  busy.value = true;
  try {
    await api('power', 'POST', { enabled });
    notice(enabled ? 'VPN включён.' : 'VPN выключен. Автоконтроль сохранит этот выбор.');
  } finally {
    busy.value = false;
    await refreshStatus();
  }
}
export async function waitForPanelUpdate(job: Job, progress: (message: string) => void) {
  const end = Date.now() + 180000,
    epoch = sessionGeneration();
  while (Date.now() < end && epoch === sessionGeneration()) {
    await new Promise((r) => setTimeout(r, 1000));
    const c = new AbortController(),
      timer = setTimeout(() => c.abort(), 4000);
    try {
      const res = await fetch('/api/update-status', { cache: 'no-store', signal: c.signal }),
        d = (await res.json()) as { job: Job };
      if (d.job?.id !== job.id) continue;
      progress(d.job.message || '');
      if (d.job.phase === 'succeeded') {
        try {
          localStorage.setItem('darkls-update-message', 'Панель обновлена. Войди снова.');
        } catch {}
        location.reload();
        return;
      }
      if (['error', 'rolled_back'].includes(d.job.phase || ''))
        throw new Error(d.job.message || 'Не удалось обновить панель');
    } catch (e) {
      if (e instanceof Error && !(e instanceof TypeError) && e.name !== 'AbortError') throw e;
    } finally {
      clearTimeout(timer);
    }
  }
  throw new Error('Ожидание завершено. Обнови страницу и проверь версию панели.');
}
export const probeFacts = computed(() => {
  const p = status.value.probe || {};
  return [
    [
      'HTTPS через VPN',
      status.value.probe_fresh
        ? p.ok
          ? 'Запрос прошёл'
          : 'Ошибка запроса'
        : 'Нет свежей проверки',
    ],
    ['Последний успех', stamp(p.last_success)],
    ['Последняя ошибка', stamp(p.last_failure)],
    ['Последовательных неудач', p.failures || 0],
    ['Восстановлений с перезагрузки', p.recoveries || 0],
    ['Последнее восстановление', stamp(p.last_recovery)],
    ['Причина', p.recovery_reason || '—'],
    ['Длительность проверки', p.duration_ms === undefined ? '—' : p.duration_ms + ' мс'],
    ['Ошибка', p.error || '—'],
  ] as [string, string | number][];
});
