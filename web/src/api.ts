// All credentials remain in memory. A session generation rejects responses from
// a previous login, including slow secret-reveal requests completing after logout.
let csrf = '';
let generation = 0;
let expired = () => {};
const pending = new Set<AbortController>();
export function onExpired(fn: () => void) {
  expired = fn;
}
export function setSession(token: string) {
  for (const c of pending) c.abort();
  pending.clear();
  csrf = token;
  generation++;
}
export function sessionGeneration() {
  return generation;
}
export class StaleSession extends Error {}
export async function api<T>(
  path: string,
  method: 'GET' | 'POST' = 'GET',
  body?: unknown,
  multipart = false
): Promise<T> {
  const epoch = generation,
    controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), path === 'status' ? 8000 : 180000);
  pending.add(controller);
  try {
    const headers: Record<string, string> = {};
    if (method === 'POST') headers['X-CSRF-Token'] = csrf;
    if (body !== undefined && !multipart) headers['Content-Type'] = 'application/json';
    const response = await fetch('/api/' + path, {
      method,
      headers,
      credentials: 'same-origin',
      signal: controller.signal,
      body: body === undefined ? undefined : multipart ? (body as FormData) : JSON.stringify(body),
    });
    if (epoch !== generation) throw new StaleSession('Сеанс изменился; войди заново.');
    let result: T & { error?: string };
    try {
      result = await response.json();
    } catch {
      throw new Error('Нет ответа от панели. Проверь подключение к роутеру.');
    }
    if (epoch !== generation) throw new StaleSession('Сеанс изменился; войди заново.');
    if (!response.ok) {
      if (response.status === 401 && path !== 'login') expired();
      throw new Error(result.error || 'Ошибка запроса');
    }
    return result;
  } catch (e) {
    if (epoch !== generation) throw new StaleSession('Сеанс изменился; войди заново.');
    if (e instanceof DOMException && e.name === 'AbortError')
      throw new Error('Панель не ответила вовремя. Обнови страницу перед повторным применением.');
    if (e instanceof TypeError)
      throw new Error('Нет связи с панелью. Проверь подключение к роутеру.');
    throw e;
  } finally {
    clearTimeout(timer);
    pending.delete(controller);
  }
}
