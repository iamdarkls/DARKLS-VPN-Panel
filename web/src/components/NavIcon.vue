<script setup lang="ts">
defineProps<{ name: string }>();
const paths: Record<string, string> = {
  sites: 'M4 5h16v14H4z M4 9h16 M8 5v14',
  devices: 'M3 4h18v12H3z M8 20h8 M12 16v4',
  servers: 'M4 3h16v7H4z M4 14h16v7H4z M7 6h1 M7 17h1',
  diagnostics: 'M3 12h4l3-8 4 16 3-8h4',
  stability: 'M12 3l8 3v6c0 4-5 8-8 9-3-1-8-5-8-9V6z M8 12l3 3 5-6',
  tuning: 'M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6',
  updates: 'M12 3v12 M7 10l5 5 5-5 M4 16v5h16v-5',
  backups: 'M4 5h16v16H4z M3 3h18v5H3 M9 12h6',
  metrics: 'M3 3v18h18 M7 16v-5 M12 16V7 M17 16v-8',
  logs: 'M8 5h13 M8 12h13 M8 19h13 M3 5h1 M3 12h1 M3 19h1',
  settings: 'M8 10V6a4 4 0 018 0v4 M5 10h14v11H5z M12 14v3',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
};
</script>
<template>
  <svg
    class="nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[name] || paths.menu" />
  </svg>
</template>
