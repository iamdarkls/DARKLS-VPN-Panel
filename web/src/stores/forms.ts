import { computed, onUnmounted, ref, type ComputedRef } from 'vue';
// Only dirty flags are shared; form values and credentials stay with their owner.
const forms = new Set<ComputedRef<boolean>>();
const epoch = ref(0);
export const hasUnsavedForms = computed(() => {
  void epoch.value;
  return [...forms].some((dirty) => dirty.value);
});
export function registerUnsaved(check: () => boolean) {
  const dirty = computed(check);
  forms.add(dirty);
  epoch.value++;
  onUnmounted(() => {
    forms.delete(dirty);
    epoch.value++;
  });
  return dirty;
}
