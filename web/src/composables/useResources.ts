import { computed, ref } from 'vue';
import { api } from '../api';
import { mib, bytes } from './ui';

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

/** Router metrics and display values; measurements are requested by the active workspace.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useResources() {
  const metrics = ref<Metrics>();
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
  async function refresh() {
    metrics.value = await api('metrics');
  }
  return { values, refresh };
}
