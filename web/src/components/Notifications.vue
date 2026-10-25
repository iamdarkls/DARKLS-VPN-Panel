<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue';
import ToastItem from './ToastItem.vue';
import Modal from './Modal.vue';
import { toasts, toastHistory } from '../composables/ui';
const target = ref<Element>(document.body),
  historyOpen = ref(false);
let observer: MutationObserver;
onMounted(() => {
  observer = new MutationObserver(() => {
    void nextTick(() => {
      target.value = [...document.querySelectorAll('dialog[open]')].at(-1) || document.body;
    });
  });
  observer.observe(document.body, { subtree: true, attributes: true, attributeFilter: ['open'] });
});
onUnmounted(() => observer.disconnect());
</script>
<template>
  <button type="button" class="secondary notification-trigger" @click="historyOpen = true">
    Уведомления
  </button>
  <Modal
    :open="historyOpen"
    label="Уведомления"
    class="notification-dialog"
    @close="historyOpen = false"
  >
    <div class="dialog-heading">
      <h2>Уведомления</h2>
      <button
        class="icon-button"
        aria-label="Закрыть историю уведомлений"
        @click="historyOpen = false"
      >
        ×
      </button>
    </div>
    <p v-if="!toastHistory.length">Новых сообщений пока нет.</p>
    <template v-if="historyOpen">
      <div v-for="t in toastHistory" :key="t.id" class="notification-entry">
        <strong>{{ t.title }}</strong>
        <p>{{ t.text }}</p>
        <span class="tiny muted">{{ new Date(t.at).toLocaleTimeString('ru-RU') }}</span>
      </div>
    </template>
  </Modal>
  <Teleport :to="target">
    <section class="toast-region" aria-label="Уведомления">
      <ToastItem v-for="t in toasts" :key="t.id" :item="t" />
    </section>
  </Teleport>
</template>
