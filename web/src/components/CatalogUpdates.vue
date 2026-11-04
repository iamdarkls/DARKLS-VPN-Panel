<script setup lang="ts">
import PanelBox from './PanelBox.vue';
import { useCatalogUpdates } from '../composables/useCatalogUpdates';
const { showReviewed, reviewed, pending, visible, decide, busy } = useCatalogUpdates();
</script>
<template>
  <PanelBox
    title="Обновления каталога"
    description="Выбери дополнения отдельно для каждой группы. Новые группы выключены; изменения вступят в силу после обычного применения с предпросмотром."
  >
    <p role="status">Предложений: {{ pending.length }}</p>
    <label class="quic-label">
      <input v-model="showReviewed" type="checkbox" />
      Показать рассмотренные предложения
    </label>
    <details v-for="item in visible" :key="item.id">
      <summary>
        {{ item.group.name }} · {{ item.new ? 'новая группа' : 'новые домены' }}:
        {{ item.missing.length }}{{ reviewed(item) ? ' · рассмотрено' : '' }}
      </summary>
      <pre class="log-box">{{ item.missing.join('\n') }}</pre>
      <button type="button" class="primary" :disabled="busy" @click="decide(item, true)">
        Добавить в черновик
      </button>
      <button type="button" class="secondary" :disabled="busy" @click="decide(item, false)">
        Отклонить
      </button>
    </details>
    <p v-if="!visible.length" class="muted">
      Новых предложений нет. Каталог проверяется после обновления панели.
    </p>
  </PanelBox>
</template>
