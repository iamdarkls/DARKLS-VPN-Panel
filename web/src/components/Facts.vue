<script setup lang="ts">
import type { Fact } from '../types';
defineProps<{ rows: Fact[] }>();
</script>
<template>
  <div class="facts">
    <div v-for="([label, value], i) in rows" :key="i" class="fact">
      <span class="muted">{{ label }}</span>
      <strong>{{ value ?? '—' }}</strong>
    </div>
  </div>
</template>
