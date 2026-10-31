import { computed, ref } from 'vue';
import { api, sessionGeneration } from '../api';
import type { Status, Device, Activity } from '../types';
import { auth, busy } from './sessionState';
import { notice, stamp } from '../composables/ui';
export const status = ref<Status>({}),
  devices = ref<Device[]>([]),
  activity = ref<Activity>({});
let statusRequest: Promise<void> | undefined,
  activityRequest: Promise<void> | undefined,
  activityAt = 0;
const shown = new Set<string>();
let read = '';
try {
  read = localStorage.getItem('darkls-notifications-read') || '';
} catch {}
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
export async function setPower() {
  const epoch = sessionGeneration();
  const enabled = !!status.value.disabled;
  if (!enabled && !confirm('Выключить VPN для домашней сети?')) return;
  busy.value = true;
  try {
    await api('power', 'POST', { enabled });
    notice(enabled ? 'VPN включён.' : 'VPN выключен. Автоконтроль сохранит этот выбор.');
  } finally {
    if (epoch === sessionGeneration()) {
      busy.value = false;
      await refreshStatus();
    }
  }
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
export function resetVPN() {
  status.value = {};
  devices.value = [];
  activity.value = {};
  activityAt = 0;
  shown.clear();
  statusRequest = undefined;
  activityRequest = undefined;
}
