import { computed, ref, onMounted } from 'vue';
import { api } from '../api';
import { stamp, useAction, usePolling } from './ui';

interface Logs {
  events: { at: number; message: string }[];
  watch: string;
  panel: string;
  update: string;
}

/** Dialog-local filters and coalesced requests; polling belongs to the mounted dialog.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useLogsDialog() {
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
    return source.value === 'events'
      ? filtered.slice(0, limit.value)
      : filtered.slice(-limit.value);
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
  return {
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
  };
}
