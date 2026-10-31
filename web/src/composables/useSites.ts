import { computed, reactive, ref } from 'vue';
import type { Group } from '../types';
import { draft, catalogNotes, busy, temporaryGroup } from '../store';
import { stamp } from './ui';

/** Catalog filtering and group drafts; changes stay local until the shared apply transaction.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useSites() {
  const categories: Record<string, string> = {
    video: 'Видео',
    ai: 'Искусственный интеллект',
    social: 'Соцсети',
    messaging: 'Мессенджеры',
    music: 'Музыка',
    reading: 'Чтение и авторы',
    news: 'Новости и СМИ',
    tools: 'Сервисы и инструменты',
    work: 'Работа и дизайн',
    adult: '18+',
    custom: 'Мои группы',
  };
  const icons: Record<string, string> = {
    youtube: '▶',
    chatgpt: '✳',
    instagram: '◎',
    discord: '◒',
    gemini: '✦',
    twitter: '𝕏',
    telegram: '➤',
  };
  const search = ref(''),
    filter = ref(''),
    open = ref(false),
    editing = ref(-1),
    form = reactive({ name: '', domains: '', category: 'custom', quic: false }),
    timerGroup = ref(''),
    minutes = ref(60);
  const sections = computed(() =>
    Object.entries(categories)
      .map(([id, label]) => ({
        id,
        label,
        groups: (draft.value?.groups || [])
          .map((g, i) => ({ g, i }))
          .filter(
            ({ g }) =>
              (g.category || 'custom') === id &&
              (!filter.value || filter.value === id) &&
              (!search.value.trim() ||
                [g.name, label, ...g.domains].some((t) =>
                  t.toLowerCase().includes(search.value.toLowerCase().trim())
                ))
          ),
      }))
      .filter((s) => s.groups.length)
  );
  const summary = computed(() => {
    const gs = draft.value?.groups.filter((g) => g.enabled) || [];
    return (
      gs.length + ' групп включено · ' + new Set(gs.flatMap((g) => g.domains)).size + ' доменов'
    );
  });
  function edit(i: number) {
    editing.value = i;
    const g = draft.value?.groups[i];
    Object.assign(form, {
      name: g?.name || '',
      domains: g?.domains.join('\n') || '',
      category: g?.category || 'custom',
      quic: g?.block_quic ?? g?.id === 'youtube',
    });
    open.value = true;
  }
  function toggle(g: Group) {
    g.enabled = !g.enabled;
    g.enabled_until = 0;
    g.timed_previous = false;
  }
  function save() {
    if (!draft.value) return;
    const values = {
      name: form.name.trim(),
      domains: form.domains
        .split(/\r?\n/)
        .map((x) => x.trim())
        .filter(Boolean),
      category: form.category,
      block_quic: form.quic,
    };
    if (!values.name) return;
    if (editing.value < 0)
      draft.value.groups.push({
        id:
          'custom-' +
          Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) =>
            b.toString(16).padStart(2, '0')
          ).join(''),
        enabled: true,
        ...values,
      });
    else {
      const g = draft.value.groups[editing.value]!;
      if ((g.block_quic ?? g.id === 'youtube') !== form.quic) {
        g.quic_until = 0;
        g.quic_previous = false;
      }
      Object.assign(g, values);
    }
    open.value = false;
  }
  function remove() {
    if (draft.value && confirm('Удалить группу «' + form.name + '»?')) {
      draft.value.groups.splice(editing.value, 1);
      open.value = false;
    }
  }
  return {
    search,
    filter,
    categories,
    busy,
    edit,
    summary,
    sections,
    icons,
    toggle,
    catalogNotes,
    stamp,
    temporaryGroup,
    timerGroup,
    draft,
    minutes,
    open,
    save,
    editing,
    form,
    remove,
  };
}
