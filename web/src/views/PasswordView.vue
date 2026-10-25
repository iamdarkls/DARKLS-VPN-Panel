<script setup lang="ts">
import { reactive } from 'vue';
import { api } from '../api';
import { auth, dirty, logoutLocal } from '../store';
import { useAction } from '../composables/ui';
import PanelBox from '../components/PanelBox.vue';
const form = reactive({ current: '', password: '', confirm: '' });
const { pending, run } = useAction();
async function save() {
  if (form.password !== form.confirm) throw new Error('Новые пароли не совпадают.');
  if (
    dirty.value &&
    !confirm(
      'Есть несохранённые изменения. После смены пароля потребуется повторный вход. Продолжить?'
    )
  )
    return;
  try {
    await api('password', 'POST', { current: form.current, password: form.password });
    logoutLocal();
    auth.error = 'Пароль изменён. Войди с новым паролем.';
  } finally {
    form.current = '';
    form.password = '';
    form.confirm = '';
  }
}
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
