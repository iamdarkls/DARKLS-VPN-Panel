<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { api } from '../api';
import { notice, stamp, useAction } from '../composables/ui';
import { registerUnsaved } from '../stores/forms';
import { exclusiveOperation, refreshStatus } from '../stores/vpn';
import type { SubscriptionStatus } from '../types';
import CredentialScope from './CredentialScope.vue';
import PanelBox from './PanelBox.vue';

const props = defineProps<{ settings?: SubscriptionStatus; reload: () => Promise<void> }>();
const source = ref(''),
  profile = ref(''),
  auto = ref(false),
  password = ref('');
interface Review {
  id?: string;
  profile?: string;
  changes: string[];
}
interface Catalog {
  profiles: { name: string }[];
  unavailable: number;
  truncated: boolean;
}
const review = ref<Review>();
const catalog = ref<Catalog>();
const hasCatalogChoice = computed(
  () => !catalog.value || catalog.value.profiles.some((item) => item.name === profile.value)
);
watch(
  source,
  () => {
    catalog.value = undefined;
  },
  { flush: 'sync' }
);
const dirty = computed(
  () =>
    !!source.value.trim() ||
    profile.value !== (props.settings?.profile || '') ||
    auto.value !== !!props.settings?.auto
);
watch([source, profile, auto, () => props.settings?.bound], () => {
  review.value = undefined;
});
const { pending, run } = useAction();
watch(
  [() => props.settings?.profile, () => props.settings?.auto],
  ([name, enabled]) => {
    profile.value = name || '';
    auto.value = enabled || false;
  },
  { immediate: true }
);
registerUnsaved(() => dirty.value);
onUnmounted(() => {
  source.value = '';
  password.value = '';
});
const results: Record<string, string> = {
  updated: 'Профиль обновлён и проверен',
  unchanged: 'Изменений нет',
  error: 'Обновить профиль не удалось',
};
async function submit(action: 'list' | 'save' | 'preview' | 'apply' | 'remove') {
  if (!password.value) throw new Error('Введи пароль панели.');
  if ((action === 'preview' || action === 'apply') && dirty.value)
    throw new Error('Сначала сохрани настройки подписки.');
  if (
    action === 'apply' &&
    (!review.value?.id || !confirm('Применить показанные изменения? VPN переподключится.'))
  )
    return;
  if (action === 'remove' && !confirm('Удалить ссылку? Текущее подключение сохранится.')) return;
  try {
    if (action === 'list') {
      const url = source.value;
      catalog.value = undefined;
      const list = await api<Catalog>('subscription', 'POST', {
        action,
        url,
        password: password.value,
      });
      if (source.value !== url) return;
      catalog.value = list;
      if (!hasCatalogChoice.value)
        profile.value = list.profiles.length === 1 ? list.profiles[0].name : '';
      notice(list.profiles.length ? 'Профили подписки загружены.' : 'Нет профилей для выбора.');
      return;
    }
    if (action === 'preview') {
      review.value = undefined;
      review.value = await api<Review>('subscription', 'POST', {
        action,
        password: password.value,
      });
      notice(
        review.value.id
          ? 'Изменения найдены. Проверь их перед применением.'
          : 'В подписке нет изменений.'
      );
      return;
    }
    const result = await api<{ code?: string }>('subscription', 'POST', {
      action,
      password: password.value,
      ...(action === 'apply' ? { review: review.value?.id } : {}),
      ...(action === 'save' ? { url: source.value, profile: profile.value, auto: auto.value } : {}),
    });
    if (action === 'save' || action === 'remove') {
      source.value = '';
      if (action === 'remove') catalog.value = undefined;
      profile.value = action === 'save' ? profile.value.trim() : '';
      if (action === 'remove') auto.value = false;
    }
    review.value = undefined;
    if (result.code === 'updated') await refreshStatus();
    notice(
      result.code
        ? results[result.code] || 'Подписка проверена'
        : action === 'save'
          ? 'Подписка сохранена и привязана к основному профилю.'
          : 'Подписка удалена. VPN не изменён.'
    );
  } finally {
    password.value = '';
    if (action !== 'preview' && action !== 'list') await props.reload().catch(() => undefined);
  }
}
</script>

<template>
  <PanelBox
    title="VPN-подписка"
    description="Панель принимает HTTPS-подписку со списком VLESS. Сначала можно посмотреть изменения. Только применение проверяет новый туннель и переподключает VPN. Используй только подписку, которой доверяешь."
  >
    <p v-if="settings?.configured" class="muted">
      {{
        settings.bound
          ? 'Привязана к текущему профилю.'
          : 'Приостановлена: выбран другой профиль. Для новой привязки сохрани настройки.'
      }}
      {{ settings.auto ? 'Автообновление включено.' : 'Автообновление выключено.' }}
    </p>
    <p v-if="settings?.result.code" class="muted">
      {{ stamp(settings.result.checked_at) }} · {{ results[settings.result.code] }}
      <span v-if="settings.result.message">— {{ settings.result.message }}</span>
    </p>
    <div>
      <label>
        HTTPS-ссылка
        <input
          v-model="source"
          name="vpn-subscription-source"
          type="url"
          autocomplete="off"
          maxlength="4096"
          :placeholder="
            settings?.configured ? 'Оставь пустым, чтобы сохранить прежнюю ссылку' : 'https://…'
          "
        />
      </label>
      <label v-if="catalog">
        Профиль из подписки
        <select
          v-model="profile"
          name="vpn-subscription-choice"
          :disabled="!catalog.profiles.length"
        >
          <option v-if="!catalog.profiles.some((item) => item.name === '')" value="" disabled>
            {{ catalog.profiles.length ? 'Выбери VLESS-профиль' : 'Нет профилей для выбора' }}
          </option>
          <option v-for="item in catalog.profiles" :key="item.name" :value="item.name">
            {{ item.name || 'Единственный VLESS-профиль' }}
          </option>
        </select>
      </label>
      <label v-else>
        Точное имя профиля
        <input
          v-model="profile"
          name="vpn-subscription-profile"
          autocomplete="off"
          maxlength="60"
          placeholder="Можно оставить пустым, если VLESS-профиль один"
        />
      </label>
      <div v-if="catalog" class="muted" aria-live="polite">
        <p v-if="catalog.unavailable">
          Пропущено профилей: {{ catalog.unavailable }}. Нужны поддерживаемые VLESS-профили с
          различимыми именами; в списке из нескольких профилей имя обязательно.
        </p>
        <p v-if="catalog.truncated">Показаны первые {{ catalog.profiles.length }} профилей.</p>
        <button type="button" :disabled="pending" @click="catalog = undefined">
          Ввести имя вручную
        </button>
      </div>
      <label class="quic-label">
        <input v-model="auto" type="checkbox" />
        Проверять и применять изменения автоматически (раз в 6 часов)
      </label>
      <CredentialScope>
        <label>
          Пароль панели
          <input
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
          />
        </label>
      </CredentialScope>
    </div>
    <button
      type="button"
      :disabled="pending || !password || (!source.trim() && !settings?.configured)"
      @click="run(() => submit('list'))"
    >
      Загрузить профили
    </button>
    <button
      type="button"
      class="primary"
      :disabled="pending || !password || !hasCatalogChoice"
      @click="run(() => submit('save'))"
    >
      Сохранить и привязать
    </button>
    <button
      type="button"
      :disabled="pending || !password || !settings?.bound || dirty"
      @click="run(() => submit('preview'))"
    >
      Проверить подписку
    </button>
    <div v-if="review" class="result-box" aria-live="polite">
      <p>Профиль: {{ review.profile || 'Единственный VLESS-профиль' }}</p>
      <p v-if="!review.changes.length">Изменений нет.</p>
      <template v-else>
        <p>Изменились: {{ review.changes.join(', ') }}.</p>
        <p class="muted">Рабочий VPN пока не изменён. Предпросмотр действителен пять минут.</p>
        <button
          type="button"
          class="primary"
          :disabled="pending || !password || dirty || exclusiveOperation"
          @click="run(() => submit('apply'))"
        >
          Применить изменения
        </button>
      </template>
    </div>
    <button
      type="button"
      :disabled="pending || !password || !settings?.configured"
      @click="run(() => submit('remove'))"
    >
      Удалить подписку
    </button>
  </PanelBox>
</template>
