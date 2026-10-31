import { reactive } from 'vue';
import { api } from '../api';
import { auth, dirty, logoutLocal } from '../store';
import { useAction } from './ui';

/** Password-change flow; clear secret fields even when the request fails.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function usePassword() {
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
  return { run, save, form, pending };
}
