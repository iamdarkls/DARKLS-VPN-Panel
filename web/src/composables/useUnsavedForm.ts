import { onUnmounted, ref } from 'vue';
import { registerUnsaved } from '../stores/forms';

/** Snapshot stays in memory and excludes the panel password. Cancellation leaves the form intact. */
export function useUnsavedForm(snapshot: () => unknown, active: () => boolean = () => true) {
  const baseline = ref('');
  const encode = () => JSON.stringify(snapshot());
  const dirty = registerUnsaved(() => active() && baseline.value !== encode());
  function markClean() {
    baseline.value = encode();
  }
  function confirmDiscard() {
    return !dirty.value || confirm('Есть несохранённые изменения формы. Закрыть без сохранения?');
  }
  markClean();
  onUnmounted(() => {
    baseline.value = '';
  });
  return {
    dirty,
    markClean,
    confirmDiscard,
    forget: () => {
      baseline.value = '';
    },
  };
}
