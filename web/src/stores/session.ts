import { api, onExpired, setSession } from '../api';
import { auth, busy } from './sessionState';
import { resetDraft, loadState } from './draft';
import { resetVPN, refreshStatus } from './vpn';
import { clearNotifications } from '../composables/ui';
export { auth, busy } from './sessionState';
export function logoutLocal() {
  setSession('');
  auth.epoch++;
  auth.loggedIn = false;
  resetDraft();
  resetVPN();
  busy.value = false;
  clearNotifications();
}
onExpired(logoutLocal);
export async function openSession() {
  const d = await api<{ csrf: string; version: string }>('session');
  setSession(d.csrf);
  auth.epoch++;
  auth.version = d.version;
  auth.loggedIn = true;
  await loadState();
  await refreshStatus();
}
export async function initialize() {
  try {
    const message = localStorage.getItem('darkls-update-message');
    if (message) {
      auth.error = message;
      localStorage.removeItem('darkls-update-message');
    }
  } catch {}
  try {
    await openSession();
  } catch {
    logoutLocal();
  } finally {
    auth.ready = true;
  }
}
export async function login(user: string, password: string) {
  await api('login', 'POST', { user, password });
  auth.error = '';
  await openSession();
}
