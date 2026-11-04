import { computed, ref } from 'vue';
import type { CatalogUpdate } from '../types';
import { catalogUpdates, draft } from '../stores/draft';
import { busy } from '../stores/sessionState';
import { clone, notice } from './ui';

export function useCatalogUpdates() {
  const showReviewed = ref(false);
  const reviewed = (item: CatalogUpdate) => draft.value?.catalog_choices?.[item.id] === item.digest;
  const proposals = computed(() =>
    catalogUpdates.value
      .map((item) => {
        const group = draft.value?.groups.find((g) => g.id === item.group.id);
        return { ...item, missing: item.missing.filter((d) => !group?.domains.includes(d)) };
      })
      .filter((item) => item.missing.length)
  );
  const pending = computed(() => proposals.value.filter((item) => !reviewed(item)));
  const visible = computed(() => (showReviewed.value ? proposals.value : pending.value));
  function decide(item: CatalogUpdate, accept: boolean) {
    const state = draft.value;
    if (!state || busy.value) return;
    if (accept) {
      let group = state.groups.find((g) => g.id === item.group.id);
      if (!group && !item.new) {
        notice('Группа удалена из черновика. Обнови сведения перед добавлением.', true);
        return;
      }
      if (!group) {
        if (state.groups.length >= 100) {
          notice('Сначала освободи место: допускается не более 100 групп.', true);
          return;
        }
        group = clone(item.group);
        state.groups.push(group);
      }
      group.domains = [...new Set([...group.domains, ...item.missing])];
    }
    state.catalog_choices ||= {};
    state.catalog_choices[item.id] = item.digest;
    notice(
      accept
        ? 'Домены добавлены в черновик. Нажми «Применить изменения».'
        : 'Предложение отклонено в черновике. Примени изменения, чтобы сохранить решение.'
    );
  }

  return { showReviewed, reviewed, pending, visible, decide, busy };
}
