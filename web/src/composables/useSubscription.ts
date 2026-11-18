import { computed, onUnmounted, ref, watch } from 'vue';
import { api } from '../api';
import { notice, useAction } from './ui';
import { registerUnsaved } from '../stores/forms';
import { refreshStatus } from '../stores/vpn';
import type { SubscriptionStatus } from '../types';

/** One form owns its credentials, draft and preview. Fetching a catalog never applies a profile. */
export function useSubscription(
  settings: () => SubscriptionStatus | undefined,
  reload: () => Promise<void>
) {
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
      profile.value !== (settings()?.profile || '') ||
      auto.value !== !!settings()?.auto
  );
  watch([source, profile, auto, () => settings()?.bound], () => {
    review.value = undefined;
  });
  const { pending, run } = useAction();
  watch(
    [() => settings()?.profile, () => settings()?.auto],
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
        ...(action === 'save'
          ? { url: source.value, profile: profile.value, auto: auto.value }
          : {}),
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
      if (action !== 'preview' && action !== 'list') await reload().catch(() => undefined);
    }
  }

  return {
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
  };
}
