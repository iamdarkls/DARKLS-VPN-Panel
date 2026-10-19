'use strict';
// A Toast owns its timer and DOM node; the centre owns the bounded collection.
class Toast {
  constructor(center, item, {persistent, onClose}) {
    this.center = center;
    this.item = item;
    this.persistent = persistent;
    this.onClose = onClose;
    this.remaining = 8000;
    this.timer = null;
    this.removed = false;
    this.node = el('article', 'toast' + (item.error ? ' toast-error' : ''));
    this.node.setAttribute('role', item.error ? 'alert' : 'status');
    const mark = el('span', 'toast-mark', item.error ? '!' : '✓');
    const body = el('div', 'toast-body');
    body.append(el('strong', '', item.title), el('p', '', item.text));
    const close = el('button', 'toast-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', 'Закрыть уведомление');
    close.onclick = () => this.remove(true);
    this.node.addEventListener('mouseenter', () => this.pause());
    this.node.addEventListener('mouseleave', () => this.resume());
    this.node.addEventListener('focusin', () => this.pause());
    this.node.addEventListener('focusout', () => setTimeout(() => this.resume(), 0));
    this.node.append(mark, body, close);
  }

  pause() {
    if (!this.timer) return;
    clearTimeout(this.timer);
    this.timer = null;
    this.remaining = Math.max(0, this.remaining - (Date.now() - this.started));
  }

  resume() {
    if (this.timer || this.persistent || this.removed || this.node.matches(':hover,:focus-within')) return;
    this.started = Date.now();
    this.timer = setTimeout(() => this.remove(false), this.remaining);
  }

  remove(acknowledged) {
    if (this.removed) return;
    this.removed = true;
    this.pause();
    this.node.remove();
    this.center.nodes.delete(this.item.id);
    if (acknowledged) this.onClose?.();
  }
}

class NotificationCenter {
  constructor() {
    this.history = [];
    this.nodes = new Map();
    this.serial = 0;
    this.region = el('section', 'toast-region');
    this.region.setAttribute('aria-label', 'Уведомления');
    document.body.append(this.region);
    this.dialog = el('dialog', 'notification-dialog');
    this.dialog.setAttribute('aria-labelledby', 'notification-title');
    const heading = el('div', 'dialog-heading');
    const title = el('h2', '', 'Уведомления');
    title.id = 'notification-title';
    const close = el('button', 'icon-button', '×');
    close.type = 'button';
    close.setAttribute('aria-label', 'Закрыть историю уведомлений');
    close.onclick = () => this.dialog.close();
    heading.append(title, close);
    this.contents = el('div');
    this.dialog.append(heading, el('p', 'muted', 'Последние сообщения в этой вкладке. События VPN также доступны в разделе «Контроль связи».'), this.contents);
    document.body.append(this.dialog);
    const button = el('button', 'secondary notification-trigger', 'Уведомления');
    button.type = 'button';
    button.onclick = () => this.showHistory();
    document.querySelector('header').append(button);
    // Dialog top layers must not hide notifications or move keyboard focus.
    document.addEventListener('close', () => this.restoreRegion(), true);
    if (window.ResizeObserver) {
      this.observer = new ResizeObserver(() => {
        document.documentElement.style.setProperty('--savebar-space', $('savebar').offsetHeight + 'px');
      });
      this.observer.observe($('savebar'));
    }
  }

  restoreRegion() {
    const top = [...document.querySelectorAll('dialog[open]')].at(-1);
    (top || document.body).append(this.region);
  }

  show(text, {id, error = false, persistent = error, title, onClose} = {}) {
    if (!text) return;
    id ||= 'action-' + (++this.serial);
    if (this.nodes.has(id)) return;
    if (document.querySelector('dialog[open]')) {
      for (const toast of [...this.nodes.values()]) toast.remove(false);
    }
    this.restoreRegion();
    const item = {id, text:String(text), error, title:title || (error ? 'Не удалось выполнить' : 'Панель DARKLS'), at:Date.now()};
    this.history.unshift(item);
    this.history.splice(30);
    while (this.nodes.size >= 3) this.nodes.values().next().value.remove(false);
    const toast = new Toast(this, item, {persistent, onClose});
    this.nodes.set(id, toast);
    this.region.append(toast.node);
    toast.resume();
  }

  showHistory() {
    this.contents.replaceChildren();
    if (!this.history.length) this.contents.append(el('p', 'muted', 'Новых сообщений пока нет.'));
    for (const item of this.history) {
      const row = el('div', 'notification-entry' + (item.error ? ' error' : ''));
      row.append(el('strong', '', item.title), el('p', '', item.text), el('span', 'tiny muted', new Date(item.at).toLocaleTimeString('ru-RU')));
      this.contents.append(row);
    }
    this.dialog.showModal();
  }

  clear() {
    for (const toast of [...this.nodes.values()]) toast.remove(false);
    this.history.length = 0;
    if (this.dialog.open) this.dialog.close();
    this.contents.replaceChildren();
  }
}

const notificationCenter = new NotificationCenter();
function toastMessage(text, options) { notificationCenter.show(text, options) }
function clearToasts() { notificationCenter.clear() }
for (const item of window.pendingToasts || []) toastMessage(item.text, {error:item.error});
window.pendingToasts = [];
