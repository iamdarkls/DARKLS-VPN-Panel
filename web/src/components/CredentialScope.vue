<script setup lang="ts">
import CredentialIdentity from './CredentialIdentity.vue';
</script>
<template>
  <form class="credential-scope" autocomplete="on" @submit.prevent>
    <CredentialIdentity />
    <slot />
  </form>
</template>
<style scoped>
.credential-scope {
  display: contents;
}
</style>
