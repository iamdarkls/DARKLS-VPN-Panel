import { sessionGeneration } from '../api';
import type { Job } from '../types';
export async function waitForPanelUpdate(job: Job, progress: (message: string) => void) {
  const end = Date.now() + 180000,
    epoch = sessionGeneration();
  while (Date.now() < end && epoch === sessionGeneration()) {
    await new Promise((r) => setTimeout(r, 1000));
    const c = new AbortController(),
      timer = setTimeout(() => c.abort(), 4000);
    try {
      const res = await fetch('/api/update-status', { cache: 'no-store', signal: c.signal }),
        d = (await res.json()) as { job: Job };
      if (epoch !== sessionGeneration()) return;
      if (d.job?.id !== job.id) continue;
      progress(d.job.message || '');
      if (d.job.phase === 'succeeded') {
        try {
          localStorage.setItem('darkls-update-message', 'Панель обновлена. Войди снова.');
        } catch {}
        location.reload();
        return;
      }
      if (['error', 'rolled_back'].includes(d.job.phase || ''))
        throw new Error(d.job.message || 'Не удалось обновить панель');
    } catch (e) {
      if (e instanceof Error && !(e instanceof TypeError) && e.name !== 'AbortError') throw e;
    } finally {
      clearTimeout(timer);
    }
  }
  throw new Error('Ожидание завершено. Обнови страницу и проверь версию панели.');
}
