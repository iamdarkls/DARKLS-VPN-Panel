import { ref, watch } from 'vue';
export type Theme = 'auto' | 'light' | 'dark';
const valid = (s: string | null): s is Theme => s === 'auto' || s === 'light' || s === 'dark';
let initial: Theme = 'auto';
try {
  const saved = localStorage.getItem('darkls-panel-theme');
  if (valid(saved)) initial = saved;
} catch {}
export const theme = ref<Theme>(initial);
const media = matchMedia('(prefers-color-scheme: dark)');
function apply() {
  document.documentElement.dataset.theme =
    theme.value === 'auto' ? (media.matches ? 'dark' : 'light') : theme.value;
}
watch(
  theme,
  () => {
    apply();
    try {
      localStorage.setItem('darkls-panel-theme', theme.value);
    } catch {}
  },
  { immediate: true }
);
media.addEventListener('change', apply);
