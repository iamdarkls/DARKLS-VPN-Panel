<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import type { Toast } from '../composables/ui';
import { dismiss } from '../composables/ui';
const props = defineProps<{ item: Toast }>();
let timer: ReturnType<typeof setTimeout> | undefined,
  remaining = 8000,
  started = 0;
function pause() {
  if (timer) {
    clearTimeout(timer);
    timer = undefined;
    remaining = Math.max(0, remaining - (Date.now() - started));
  }
}
function resume() {
  if (timer || props.item.persistent) return;
  started = Date.now();
  timer = setTimeout(() => dismiss(props.item.id, false), remaining);
}
onMounted(resume);
onUnmounted(pause);
</script>
<template>
  <article
    class="toast"
    :class="{ 'toast-error': item.error }"
    :role="item.error ? 'alert' : 'status'"
    @mouseenter="pause"
    @mouseleave="resume"
    @focusin="pause"
    @focusout="resume"
  >
    <span class="toast-mark">{{ item.error ? '!' : '✓' }}</span>
    <div class="toast-body">
      <strong>{{ item.title }}</strong>
      <p>{{ item.text }}</p>
    </div>
    <button
      type="button"
      class="toast-close"
      aria-label="Закрыть уведомление"
      @click="dismiss(item.id)"
    >
      ×
    </button>
  </article>
</template>
