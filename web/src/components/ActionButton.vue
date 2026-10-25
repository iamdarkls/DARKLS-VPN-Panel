<script setup lang="ts">
import { useAction } from '../composables/ui';
const props = withDefaults(
  defineProps<{ action: () => Promise<unknown>; disabled?: boolean; kind?: string }>(),
  { kind: 'secondary' }
);
const { pending, run } = useAction();
</script>
<template>
  <button
    type="button"
    :class="kind"
    :disabled="disabled || pending"
    :aria-busy="pending"
    @click="run(props.action)"
  >
    <slot />
  </button>
</template>
