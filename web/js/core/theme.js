'use strict';
(() => {
  const storageKey = 'darkls-panel-theme';
  const modes = ['auto', 'light', 'dark'];
  const media = window.matchMedia('(prefers-color-scheme: dark)');
  let mode = 'auto';
  try {
    const saved = localStorage.getItem(storageKey);
    if (modes.includes(saved)) mode = saved
  } catch {}
  const apply = () => {
    document.documentElement.dataset.theme = mode === 'auto' ? (media.matches ? 'dark' : 'light') : mode
  };
  apply();
  media.addEventListener('change', () => {
    if (mode === 'auto') apply()
  });
  document.addEventListener('DOMContentLoaded', () => {
    const select = document.getElementById('theme-select');
    select.value = mode;
    select.addEventListener('change', () => {
      mode = modes.includes(select.value) ? select.value : 'auto';
      apply();
      try {
        localStorage.setItem(storageKey, mode)
      } catch {}
    });
  });
})();
