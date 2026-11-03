<script setup lang="ts">
import { notice, toastHistory, useAction } from '../composables/ui';
const props = withDefaults(
  defineProps<{
    action: () => Promise<unknown>;
    disabled?: boolean;
    kind?: string;
    success?: string;
  }>(),
  { kind: 'secondary' }
);
const { pending, run } = useAction();
async function execute() {
  const previous = toastHistory.value[0]?.id;
  const result = await props.action();
  if (props.success && result !== false && toastHistory.value[0]?.id === previous)
    notice(props.success);
}
</script>
<template>
  <button
    type="button"
    :class="kind"
    :disabled="disabled || pending"
    :aria-busy="pending"
    @click="run(execute)"
  >
    <slot />
  </button>
</template>
