<script setup lang="ts">
import { stamp } from '../composables/ui';
import { useSubscription } from '../composables/useSubscription';
import { exclusiveOperation } from '../stores/vpn';
import type { SubscriptionStatus } from '../types';
import CredentialScope from './CredentialScope.vue';
import PanelBox from './PanelBox.vue';

const props = defineProps<{ settings?: SubscriptionStatus; reload: () => Promise<void> }>();
const {
  source,
  profile,
  auto,
  password,
  review,
  catalog,
  hasCatalogChoice,
  dirty,
  pending,
  run,
  results,
  submit,
} = useSubscription(
  () => props.settings,
  () => props.reload()
);
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
