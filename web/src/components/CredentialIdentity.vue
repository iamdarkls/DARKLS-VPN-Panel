<script setup lang="ts">
import { auth } from '../stores/sessionState';
</script>
<template>
  <!-- The actual account anchors password-only confirmations for password managers. -->
  <input type="text" name="username" autocomplete="username" :value="auth.user" hidden />
</template>
