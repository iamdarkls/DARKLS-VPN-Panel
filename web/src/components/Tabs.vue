<script setup lang="ts">
const props = defineProps<{
  group: string;
  modelValue: string;
  items: readonly (readonly [string, string])[];
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
function key(event: KeyboardEvent, index: number) {
  const end = props.items.length - 1;
  const n = (
    {
      ArrowRight: (index + 1) % (end + 1),
      ArrowLeft: (index + end) % (end + 1),
      Home: 0,
      End: end,
    } as Record<string, number>
  )[event.key];
  if (n === undefined) return;
  event.preventDefault();
  const name = props.items[n]![0];
  emit('update:modelValue', name);
  (event.currentTarget as HTMLElement).parentElement
    ?.querySelectorAll<HTMLButtonElement>('button')
    [n]?.focus();
}
</script>
<template>
  <div class="section-tabs" role="tablist" :data-tabs="group" :aria-label="group">
    <button
      v-for="([id, label], i) in items"
      :id="'tab-' + group + '-' + id"
      :key="id"
      type="button"
      role="tab"
      :aria-selected="modelValue === id"
      :aria-controls="group + '-' + id"
      :tabindex="modelValue === id ? 0 : -1"
      @click="emit('update:modelValue', id)"
      @keydown="key($event, i)"
    >
      {{ label }}
    </button>
  </div>
</template>
