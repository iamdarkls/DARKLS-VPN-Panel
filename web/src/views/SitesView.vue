<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import type { Group } from '../types';
import { draft, catalogNotes, busy, temporaryGroup } from '../store';
import { stamp } from '../composables/ui';
import Modal from '../components/Modal.vue';
import ActionButton from '../components/ActionButton.vue';
import PanelBox from '../components/PanelBox.vue';
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
  return gs.length + ' групп включено · ' + new Set(gs.flatMap((g) => g.domains)).size + ' доменов';
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
defineExpose({ refresh: async () => {} });
</script>
<template>
  <section id="sites-view">
    <div class="toolbar">
      <div class="search">
        <span>⌕</span>
        <input
          id="search"
          v-model="search"
          placeholder="Найти сервис, домен или 18+"
          aria-label="Поиск сервиса или домена"
        />
      </div>
      <select id="category-filter" v-model="filter" aria-label="Назначение группы">
        <option value="">Все назначения</option>
        <option v-for="(label, id) in categories" :key="id" :value="id">{{ label }}</option>
      </select>
      <button id="add-group" class="secondary" :disabled="busy" @click="edit(-1)">
        + Новая группа
      </button>
    </div>
    <div class="section-label">
      <h2>Каталог сервисов</h2>
      <span id="count" class="muted">{{ summary }}</span>
    </div>
    <div id="groups" class="groups">
      <section v-for="section in sections" :key="section.id" class="purpose-section">
        <div class="purpose-heading">
          <h2>{{ section.label }}</h2>
          <span class="muted tiny">{{ section.groups.length }} групп</span>
        </div>
        <div class="groups-grid">
          <article
            v-for="{ g, i } in section.groups"
            :key="g.id"
            class="card"
            :class="{ disabled: !g.enabled }"
          >
            <div class="card-head">
              <div class="service-icon">
                {{
                  icons[g.id] ||
                  (g.id.startsWith('adult-') ? '18+' : g.name.slice(0, 1).toUpperCase())
                }}
              </div>
              <div class="card-title">
                <h3>{{ g.name }}</h3>
                <div class="card-meta">
                  {{ g.domains.length }} доменов{{ g.id === 'telegram' ? ' + IP-сети' : '' }}
                </div>
              </div>
              <button
                class="switch"
                :class="{ on: g.enabled }"
                type="button"
                role="switch"
                :aria-checked="g.enabled"
                :aria-label="g.name"
                :disabled="busy"
                @click="toggle(g)"
              ></button>
            </div>
            <div class="domain-preview">
              {{ g.domains.slice(0, 5).join(' · ') || 'Добавь домены этой группы' }}
            </div>
            <p v-if="catalogNotes[g.id]" class="tiny muted">{{ catalogNotes[g.id] }}</p>
            <div class="card-bottom">
              <span class="route-tag">{{ g.enabled ? 'ЧЕРЕЗ VPN' : 'ГРУППА ВЫКЛЮЧЕНА' }}</span>
              <button class="edit-button" :disabled="busy" @click="edit(i)">Настроить ↗</button>
            </div>
            <p v-if="g.enabled_until || g.quic_until" class="tiny muted">
              До {{ stamp(Math.max(g.enabled_until || 0, g.quic_until || 0)) }} · затем возврат
              прежнего выбора
            </p>
            <ActionButton
              kind="link-button"
              :disabled="busy"
              :action="() => temporaryGroup(g.id, 60)"
            >
              Включить на час
            </ActionButton>
          </article>
        </div>
      </section>
      <div v-if="!sections.length" class="empty">Ничего не найдено</div>
    </div>
    <details id="sites-timers" class="panel-box">
      <summary>Включить группу на заданное время</summary>
      <div id="sites-timer-content">
        <PanelBox
          title="Временно включить группу"
          description="Прежнее состояние вернётся автоматически после окончания срока и синхронизации часов."
        >
          <label>
            Группа
            <select v-model="timerGroup">
              <option v-for="g in draft?.groups" :key="g.id" :value="g.id">{{ g.name }}</option>
            </select>
          </label>
          <label>
            Срок в минутах, от 1 до 1440
            <input v-model.number="minutes" type="number" min="1" max="1440" />
          </label>
          <ActionButton
            :action="() => temporaryGroup(timerGroup || draft?.groups[0]?.id || '', minutes)"
          >
            Включить временно
          </ActionButton>
        </PanelBox>
      </div>
    </details>
    <p class="footnote">
      Домен включает поддомены. Общие адреса CDN могут затрагивать несколько сервисов. Устройство
      должно использовать DNS роутера; защищённый DNS браузера может обходить отбор. Для новых
      маршрутов перезапусти приложение. Доменные правила не гарантируют проксирование звонков по
      прямым IP.
    </p>
    <Modal id="edit-dialog" :open="open" label="Настройка группы" @close="open = false">
      <form id="edit-form" @submit.prevent="save">
        <div class="dialog-heading">
          <h2 id="edit-title">{{ editing < 0 ? 'Новая группа' : 'Домены сервиса' }}</h2>
          <button
            id="close-dialog"
            type="button"
            class="icon-button"
            aria-label="Закрыть"
            @click="open = false"
          >
            ×
          </button>
        </div>
        <label>
          Название
          <input id="group-name" v-model="form.name" maxlength="60" required />
        </label>
        <label>
          Назначение
          <select id="group-category" v-model="form.category">
            <option v-for="(label, id) in categories" :key="id" :value="id">{{ label }}</option>
          </select>
        </label>
        <label class="quic-label">
          <input id="group-quic" v-model="form.quic" type="checkbox" />
          Запретить QUIC для этой группы, использовать TCP
        </label>
        <p class="tiny muted">
          По умолчанию запрет включён только для YouTube. При ручном изменении таймер QUIC
          отменяется.
        </p>
        <label>
          Домены
          <textarea
            id="group-domains"
            v-model="form.domains"
            rows="12"
            spellcheck="false"
          ></textarea>
        </label>
        <p class="tiny muted">По одному домену в строке. Можно вставлять https-ссылки.</p>
        <div class="dialog-actions">
          <button
            v-show="editing >= 0"
            id="delete-group"
            class="danger"
            type="button"
            @click="remove"
          >
            Удалить группу
          </button>
          <button class="primary" type="submit">Готово</button>
        </div>
      </form>
    </Modal>
  </section>
</template>
