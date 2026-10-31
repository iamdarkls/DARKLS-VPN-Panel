import { computed, ref } from 'vue';
import { api, sessionGeneration } from '../api';
import type { PanelState, Preview, ApplyProgress } from '../types';
import { busy } from './sessionState';
import { refreshStatus } from './vpn';
import { clone, notice } from '../composables/ui';
export const draft = ref<PanelState>(),
  original = ref<PanelState>(),
  revision = ref(''),
  catalogNotes = ref<Record<string, string>>({});
export const dirty = computed(
  () => !!draft.value && JSON.stringify(draft.value) !== JSON.stringify(original.value)
);
export const applyProgress = ref<ApplyProgress>();
export const preview = ref<{ data: Preview; state: PanelState; revision: string }>();
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
export function cleanDraft() {
  if (dirty.value) throw new Error('Сначала примени или отмени изменения списков.');
}
export function discard() {
  if (original.value) draft.value = clone(original.value);
}
export async function prepareApply() {
  if (!draft.value) return;
  const epoch = sessionGeneration();
  busy.value = true;
  try {
    applyProgress.value = undefined;
    const captured = clone(draft.value),
      expected = revision.value;
    const data = await api<Preview>('preview', 'POST', { state: captured, revision: expected });
    preview.value = { data, state: captured, revision: expected };
  } catch (e) {
    if (epoch === sessionGeneration()) busy.value = false;
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
  const epoch = sessionGeneration();
  const id = Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) =>
    b.toString(16).padStart(2, '0')
  ).join('');
  applyProgress.value = { id, phase: 'validation', steps: [] };
  let done = false;
  async function readProgress() {
    try {
      const result = await api<ApplyProgress>('apply-status');
      const current = applyProgress.value;
      // A delayed running sample must not replace an authoritative terminal result.
      if (
        epoch === sessionGeneration() &&
        result.id === id &&
        ((!current?.finished && current?.phase !== 'unknown') || result.finished)
      )
        applyProgress.value = result;
    } catch {
      /* The POST remains authoritative; transient polling errors do not retry it. */
    }
  }
  const poll = (async () => {
    while (!done && epoch === sessionGeneration()) {
      await new Promise((r) => setTimeout(r, 750));
      if (!done) await readProgress();
    }
  })();
  try {
    const result = await api<{ progress?: ApplyProgress }>('apply', 'POST', {
      state: p.state,
      revision: p.revision,
      operation_id: id,
    });
    if (result.progress?.id === id) applyProgress.value = result.progress;
    await readProgress();
    if (!applyProgress.value?.finished)
      applyProgress.value = {
        ...applyProgress.value!,
        phase: 'succeeded',
        finished: Math.floor(Date.now() / 1000),
        saved: true,
        message:
          'Сохранение подтверждено ответом панели; подробный прогресс недоступен. Связь проверяется отдельно.',
      };
    await loadState();
    notice(
      'Изменения применены без перезапуска Xray. Для старого DNS-кеша перезапусти приложение или выполни ipconfig /flushdns на ПК.'
    );
  } catch (error) {
    await readProgress();
    if (epoch === sessionGeneration() && !applyProgress.value?.finished)
      applyProgress.value = {
        ...applyProgress.value!,
        phase: 'unknown',
        message:
          'Результат операции пока не подтверждён. Проверь состояние перед повторным применением.',
      };
    throw error;
  } finally {
    done = true;
    await poll;
    if (epoch === sessionGeneration()) {
      busy.value = false;
      await refreshStatus();
    }
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
export function resetDraft() {
  draft.value = undefined;
  original.value = undefined;
  revision.value = '';
  catalogNotes.value = {};
  preview.value = undefined;
  applyProgress.value = undefined;
}

export function dismissApplyProgress() {
  if (applyProgress.value?.finished || applyProgress.value?.phase === 'unknown')
    applyProgress.value = undefined;
}
