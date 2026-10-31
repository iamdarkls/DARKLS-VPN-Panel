<script setup lang="ts">
import { useResources } from '../composables/useResources';
const { values, refresh } = useResources();
defineProps<{ mode: 'metrics' | 'logs' }>();
defineExpose({ refresh });
</script>
<template>
  <section :id="mode + '-view'">
    <template v-if="mode === 'metrics'">
      <div id="metrics-grid" class="metrics-grid">
        <div v-for="[label, value, sub] in values" :key="label" class="metric-card">
          <span class="muted">{{ label }}</span>
          <strong>{{ value }}</strong>
          <span class="tiny muted">{{ sub }}</span>
        </div>
      </div>
      <p class="footnote">
        Память процессов — RSS. CPU — среднее между замерами. Опрос выполняется только при открытой
        вкладке панели.
      </p>
    </template>
  </section>
</template>
