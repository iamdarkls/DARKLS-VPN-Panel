<script setup lang="ts">
import { computed, ref } from 'vue';
import { api } from '../api';
import { stamp } from '../composables/ui';
import { connectionRows } from '../connectivity';
import { activity, exclusiveOperation, loadActivity, refreshStatus, status } from '../stores/vpn';
import type { Connectivity } from '../types';
import ActionButton from './ActionButton.vue';
import Facts from './Facts.vue';
import PanelBox from './PanelBox.vue';

const manual = ref<Connectivity>();
const sample = computed(() =>
  (manual.value?.at || 0) >= (status.value.connectivity?.at || 0)
    ? manual.value || status.value.connectivity
    : status.value.connectivity
);
const rows = computed(() => connectionRows(sample.value, status.value));
const oldSample = computed(() => {
  if (!sample.value?.at) return false;
  const age = Date.now() / 1000 - sample.value.at;
  return age < -5 || age > 300;
});
const recovery = computed(() => activity.value.recovery || []);
async function check() {
  manual.value = await api<Connectivity>('connectivity', 'POST', {});
  await Promise.all([refreshStatus(), loadActivity(true)]);
}
</script>

<template>
  <PanelBox
    title="Почему VPN не работает"
    description="Последние результаты с роутера и история восстановления. Сбор запускается вручную или во время автоконтроля при сбое."
  >
    <p v-if="status.disabled" class="muted">
      VPN выключен. Предыдущий замер относится к прошлому состоянию.
    </p>
    <p class="muted">
      Последний снимок диагностики: {{ stamp(sample?.at) }}
      <span v-if="oldSample">· устарел, запусти новую проверку.</span>
    </p>
    <Facts :rows="rows" />
    <p v-if="sample?.complete === false" class="muted">
      Часть результатов отсутствует. Это само по себе не означает сбой.
    </p>
    <ActionButton
      :disabled="exclusiveOperation"
      :action="check"
      success="Состояние связи обновлено."
    >
      Проверить состояние
    </ActionButton>
    <details :open="!!recovery.length">
      <summary>История восстановления за сутки</summary>
      <p v-if="!recovery.length" class="muted">Записей нет.</p>
      <p v-for="(item, index) in recovery" :key="index">
        {{ stamp(item.at) }} · {{ item.message }}
      </p>
    </details>
  </PanelBox>
</template>
