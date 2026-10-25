<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>
<template>
  <section class="panel-box">
    <h2>{{ title }}</h2>
    <p v-if="description" class="muted">{{ description }}</p>
    <slot />
  </section>
</template>
