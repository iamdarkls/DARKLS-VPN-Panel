import { computed, ref } from 'vue';
import { api } from '../api';
import { cleanDraft, temporaryGroup } from '../store';
import { services, notice } from './ui';

interface LoadResult {
  idle_ms?: number;
  load_ms?: number;
  failures: number;
  bytes: number;
  enough_samples: boolean;
  note: string;
}

interface Benchmark {
  results: {
    preset: string;
    samples: number;
    failures: number;
    median_ms: number;
    download_bytes: number;
    download_mbps: number;
    error?: string;
  }[];
  note: string;
}

/** User-triggered QUIC observations and bounded benchmarks; no background load tests.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useTuning() {
  const group = ref('instagram'),
    quic = ref('true'),
    preset = ref('reuse2'),
    password = ref(''),
    via = ref('direct');
  const benchmark = ref<Benchmark>(),
    load = ref<LoadResult>();
  const rows = computed(
    () =>
      benchmark.value?.results.map((r) => ({
        name: r.preset,
        median: r.samples - r.failures > 0 ? r.median_ms.toFixed(0) + ' мс' : 'Нет успешных проб',
        fail: r.failures + '/' + r.samples,
        speed: r.download_bytes ? r.download_mbps.toFixed(2) + ' Мбит/с' : 'Нет данных',
        error: r.error || '—',
      })) || []
  );
  async function apply() {
    cleanDraft();
    if (
      !confirm(
        'После изолированной проверки Xray перезапустится. Текущие VPN-соединения переподключатся. Продолжить?'
      )
    )
      return;
    try {
      await api('xhttp-apply', 'POST', { preset: preset.value, password: password.value });
      notice('Параметры применены после проверки. При ошибке предусмотрен откат.');
    } finally {
      password.value = '';
    }
  }
  async function observe(verdict: string) {
    await api('quic-observation', 'POST', { group: group.value, verdict });
    notice('Оценка сохранена в истории.');
  }
  async function compare() {
    cleanDraft();
    benchmark.value = await api<Benchmark>('xhttp-benchmark', 'POST', {});
  }
  async function measureLoad() {
    load.value = await api<LoadResult>('load-test', 'POST', { via: via.value });
  }
  return {
    measureLoad,
    compare,
    observe,
    group,
    services,
    quic,
    temporaryGroup,
    benchmark,
    rows,
    preset,
    password,
    apply,
    via,
    load,
  };
}
