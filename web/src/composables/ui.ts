import { onUnmounted, ref } from 'vue';
import { StaleSession } from '../api';
export interface Toast {
  id: string;
  text: string;
  error: boolean;
  title: string;
  at: number;
  persistent: boolean;
  onClose?: () => void;
}
export const toasts = ref<Toast[]>([]),
  toastHistory = ref<Toast[]>([]);
let serial = 0;
export function notice(text: string, error = false, options: Partial<Toast> = {}) {
  if (!text) return;
  const item: Toast = {
    id: options.id || 'action-' + ++serial,
    text,
    error,
    title: error ? 'Не удалось выполнить' : 'Панель DARKLS',
    at: Date.now(),
    persistent: error,
    ...options,
  };
  if (toasts.value.some((t) => t.id === item.id)) return;
  if (document.querySelector('dialog[open]')) toasts.value = [];
  toasts.value = [...toasts.value.slice(-2), item];
  toastHistory.value = [item, ...toastHistory.value].slice(0, 30);
}
export function dismiss(id: string, acknowledged = true) {
  const item = toasts.value.find((t) => t.id === id);
  toasts.value = toasts.value.filter((t) => t.id !== id);
  if (acknowledged) item?.onClose?.();
}
export function clearNotifications() {
  toasts.value = [];
  toastHistory.value = [];
}
export function errorMessage(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}
export async function perform(fn: () => Promise<unknown>) {
  try {
    await fn();
  } catch (e) {
    if (!(e instanceof StaleSession)) notice(errorMessage(e), true);
  }
}
export function useAction() {
  const pending = ref(false);
  return {
    pending,
    run: async (fn: () => Promise<unknown>) => {
      if (pending.value) return;
      pending.value = true;
      try {
        await perform(fn);
      } finally {
        pending.value = false;
      }
    },
  };
}
export const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));
export const stamp = (n?: number) =>
  n && n > 1735689600 ? new Date(n * 1000).toLocaleString('ru-RU') : 'Ещё нет данных';
export const mib = (k?: number) => (k === undefined ? '—' : (k / 1024).toFixed(1) + ' МиБ');
export const bytes = (b = 0) => (b / 1048576).toFixed(2) + ' МиБ';
export const duration = (s: unknown) => {
  const n = Math.max(0, Number(s) || 0);
  return `${Math.floor(n / 3600)} ч ${Math.floor((n % 3600) / 60)} мин ${Math.floor(n % 60)} с`;
};
export const serviceLabel = (s: string) =>
  ({
    responded: 'Сайт ответил',
    challenge: 'Проверка Cloudflare',
    timeout: 'Тайм-аут',
    dns_error: 'Ошибка DNS',
    connection_error: 'Ошибка соединения',
    http_error: 'HTTP-ошибка',
  })[s] || s;
export const services = [
  ['instagram', 'Instagram'],
  ['chatgpt', 'ChatGPT'],
  ['youtube', 'YouTube'],
  ['discord', 'Discord'],
  ['gemini', 'Gemini'],
  ['twitter', 'X / Twitter'],
  ['grok', 'Grok / xAI'],
  ['telegram', 'Telegram'],
] as const;
// One owner, no overlapping work. Requests complete before a subsequent tick.
export function usePolling(task: () => Promise<unknown>, interval: number) {
  let stopped = false,
    timer: ReturnType<typeof setTimeout> | undefined;
  async function tick() {
    if (stopped) return;
    if (!document.hidden) await perform(task);
    if (!stopped) timer = setTimeout(tick, interval);
  }
  timer = setTimeout(tick, interval);
  onUnmounted(() => {
    stopped = true;
    clearTimeout(timer);
  });
}
