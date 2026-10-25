<script setup lang="ts">
defineProps<{ columns: readonly (readonly [string, string])[]; rows: Record<string, unknown>[] }>();
</script>
<template>
  <div class="table-scroll">
    <table class="advanced-table">
      <thead>
        <tr>
          <th v-for="[key, title] in columns" :key="key">{{ title }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, i) in rows" :key="i">
          <td v-for="[key] in columns" :key="key">{{ row[key] ?? '—' }}</td>
        </tr>
      </tbody>
    </table>
    <p v-if="!rows.length" class="muted">Записей пока нет.</p>
  </div>
</template>
