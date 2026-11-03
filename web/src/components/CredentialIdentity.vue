<script setup lang="ts">
import { auth } from '../stores/sessionState';
</script>
<template>
  <!-- The actual account anchors password-only confirmations for password managers. -->
  <input
    type="text"
    name="username"
    autocomplete="username"
    :value="auth.user"
    readonly
    tabindex="-1"
    aria-hidden="true"
    class="credential-identity"
  />
</template>

<style scoped>
.credential-identity {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}
</style>
