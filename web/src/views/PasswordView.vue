<script setup lang="ts">
import PanelBox from '../components/PanelBox.vue';

import { usePassword } from '../composables/usePassword';
const { run, save, form, pending } = usePassword();
</script>
<template>
  <section id="settings-view">
    <div class="settings-grid">
      <PanelBox title="Сменить пароль панели">
        <form id="password-form" @submit.prevent="run(save)">
          <label>
            Текущий пароль
            <input
              v-model="form.current"
              name="current"
              type="password"
              autocomplete="current-password"
              required
            />
          </label>
          <label>
            Новый пароль
            <input
              v-model="form.password"
              name="password"
              type="password"
              autocomplete="new-password"
              minlength="12"
              maxlength="128"
              required
            />
          </label>
          <label>
            Повтори новый пароль
            <input
              v-model="form.confirm"
              name="confirm"
              type="password"
              autocomplete="new-password"
              minlength="12"
              maxlength="128"
              required
            />
          </label>
          <p class="tiny muted">От 12 до 128 символов. После смены все сеансы завершатся.</p>
          <button class="primary" type="submit" :disabled="pending">Сменить пароль</button>
        </form>
      </PanelBox>
    </div>
  </section>
</template>
