<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue';
const props = defineProps<{ open: boolean; pending?: boolean; label?: string }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLDialogElement>();
watch(
  () => props.open,
  async (value) => {
    await nextTick();
    if (value && !dialog.value?.open) dialog.value?.showModal();
    else if (!value && dialog.value?.open) dialog.value.close();
  },
  { immediate: true }
);
onBeforeUnmount(() => dialog.value?.close());
function cancel(event: Event) {
  event.preventDefault();
  if (!props.pending) emit('close');
}
</script>
<template>
  <dialog ref="dialog" :aria-label="label" @cancel="cancel"><slot /></dialog>
</template>
