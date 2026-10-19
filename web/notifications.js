'use strict';
// All messages stay in this tab's memory. Toasts never move keyboard focus.
const toastRegion = document.createElement('section');
toastRegion.className = 'toast-region';
toastRegion.setAttribute('aria-label', 'Уведомления');
document.body.append(toastRegion);
const toastHistory = [], toastNodes = new Map();
let toastSerial = 0;
function toastMessage(text, {id, error = false, persistent = error, title, onClose} = {}) {
  if (!text) return;
  id ||= 'action-' + (++toastSerial);
  if (toastNodes.has(id)) return;
  const topDialog = [...document.querySelectorAll('dialog[open]')].at(-1);
  if (topDialog) for (const item of [...toastNodes.values()]) item.remove(false);
  (topDialog || document.body).append(toastRegion);
  const item = {id, text:String(text), error, title:title || (error ? 'Не удалось выполнить' : 'Панель DARKLS'), at:Date.now()};
  toastHistory.unshift(item); toastHistory.splice(30);
  while (toastNodes.size >= 3) toastNodes.values().next().value.remove(false);
  const node = el('article', 'toast' + (error ? ' toast-error' : ''));
  node.setAttribute('role', error ? 'alert' : 'status');
  const mark = el('span','toast-mark',error ? '!' : '✓'), body = el('div','toast-body');
  body.append(el('strong','',item.title), el('p','',item.text));
  const close = el('button','toast-close','×'); close.type = 'button'; close.setAttribute('aria-label','Закрыть уведомление');
  let timer = null, remaining = 8000, started = 0, removed = false;
  const pause = () => { if (timer) { clearTimeout(timer); timer = null; remaining = Math.max(0,remaining-(Date.now()-started)) } };
  const resume = () => { if (!persistent && !removed && !node.matches(':hover,:focus-within')) { started = Date.now(); timer = setTimeout(()=>remove(false),remaining) } };
  const remove = acknowledged => {
    if (removed) return;
    removed = true; pause(); node.remove(); toastNodes.delete(id);
    if (acknowledged && onClose) onClose();
  };
  close.onclick = () => remove(true);
  node.addEventListener('mouseenter',pause); node.addEventListener('mouseleave',resume);
  node.addEventListener('focusin',pause); node.addEventListener('focusout',()=>setTimeout(resume,0));
  node.append(mark,body,close); toastNodes.set(id,{remove}); toastRegion.append(node); resume();
}
function clearToasts() {
  for (const item of [...toastNodes.values()]) item.remove(false);
  toastHistory.length = 0;
  if (notificationDialog.open) notificationDialog.close();
  notificationContents.replaceChildren();
}
const notificationDialog = el('dialog','notification-dialog');
notificationDialog.setAttribute('aria-labelledby','notification-title');
const notificationHeading = el('div','dialog-heading'), notificationTitle = el('h2','','Уведомления');
notificationTitle.id = 'notification-title'; notificationHeading.append(notificationTitle);
const notificationClose = el('button','icon-button','×'); notificationClose.type='button'; notificationClose.setAttribute('aria-label','Закрыть историю уведомлений');
notificationClose.onclick=()=>notificationDialog.close(); notificationHeading.append(notificationClose);
const notificationContents=el('div'); notificationDialog.append(notificationHeading,el('p','muted','Последние сообщения в этой вкладке. События VPN также доступны в разделе «Связь и восстановление».'),notificationContents);
document.body.append(notificationDialog);
const notificationButton=el('button','secondary notification-trigger','Уведомления'); notificationButton.type='button';
notificationButton.onclick=()=>{
  notificationContents.replaceChildren();
  if (!toastHistory.length) notificationContents.append(el('p','muted','Новых сообщений пока нет.'));
  for (const item of toastHistory) {
    const row=el('div','notification-entry'+(item.error?' error':'')); row.append(el('strong','',item.title),el('p','',item.text),el('span','tiny muted',new Date(item.at).toLocaleTimeString('ru-RU'))); notificationContents.append(row);
  }
  notificationDialog.showModal();
};
document.querySelector('header').append(notificationButton);
// Keep notifications above a changing-height apply bar on mobile and desktop.
if (window.ResizeObserver) new ResizeObserver(()=>document.documentElement.style.setProperty('--savebar-space',$('savebar').offsetHeight+'px')).observe($('savebar'));

for (const item of window.pendingToasts || []) toastMessage(item.text,{error:item.error});
window.pendingToasts=[];

document.addEventListener('close',()=>{if(toastRegion.closest('dialog') && !toastRegion.closest('dialog').open)document.body.append(toastRegion)},true);
