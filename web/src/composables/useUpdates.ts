import { registerUnsaved } from '../stores/forms';
import { computed, ref, type Ref } from 'vue';
import { api, sessionGeneration } from '../api';
import type { ReleaseStatus, Job } from '../types';
import { busy } from '../stores/sessionState';
import { cleanDraft } from '../stores/draft';
import { waitForPanelUpdate } from '../stores/updates';
import { stamp, notice, useAction } from './ui';

/** Signed panel updates. The caller owns the child Xray view; only its refresh contract crosses this boundary.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useUpdates(core: Ref<{ refresh: () => Promise<void> } | undefined>) {
  const github = ref<ReleaseStatus>({}),
    githubPassword = ref(''),
    channel = ref<'stable' | 'preview'>('stable'),
    channelPassword = ref(''),
    channelChanged = ref(false),
    githubProgress = ref(''),
    uploadPassword = ref(''),
    uploadFile = ref<File>(),
    uploadProgress = ref('');
  const { pending, run } = useAction();
  const latest = computed(() =>
    github.value.available && github.value.signed ? github.value.latest : ''
  );
  async function load() {
    github.value = await api<ReleaseStatus>('github');
    if (!channelChanged.value) channel.value = github.value.channel || 'stable';
  }
  async function saveChannel() {
    if (
      channel.value === 'preview' &&
      !confirm(
        'Тестовый канал включает предварительные выпуски. Установка остаётся ручной. Переключить канал?'
      )
    ) {
      channelPassword.value = '';
      return;
    }
    try {
      await api('github-channel', 'POST', {
        channel: channel.value,
        password: channelPassword.value,
      });
      channelChanged.value = false;
      await load();
      notice('Канал сохранён. Проверь доступные выпуски перед установкой.');
    } finally {
      channelPassword.value = '';
    }
  }
  async function returnStable() {
    cleanDraft();
    if (!github.value.rollback_available || !github.value.latest)
      throw new Error('Сначала выбери стабильный канал и проверь выпуск.');
    if (
      !confirm(
        'Вернуться с тестовой сборки на подписанный стабильный выпуск? Панель перезапустится. Текущие настройки сохраняются; перед изменением создаётся копия.'
      )
    ) {
      githubPassword.value = '';
      return;
    }
    await installRelease(true);
  }
  async function install() {
    await installRelease(false);
  }
  async function installRelease(rollback: boolean) {
    const epoch = sessionGeneration();
    cleanDraft();
    const version = rollback ? github.value.latest : latest.value;
    if (!version) throw new Error('Сначала проверь обновления.');
    busy.value = true;
    githubProgress.value = 'Загружаем выпуск и проверяем подпись…';
    try {
      const d = await api<{ job: Job }>('github-install', 'POST', {
        password: githubPassword.value,
        version,
        rollback,
      });
      githubPassword.value = '';
      await waitForPanelUpdate(d.job, (m) => (githubProgress.value = m));
    } finally {
      githubPassword.value = '';
      if (epoch === sessionGeneration()) busy.value = false;
    }
  }
  async function upload() {
    const epoch = sessionGeneration();
    cleanDraft();
    if (!uploadFile.value || uploadFile.value.size > 8 * 1024 * 1024)
      throw new Error('Выбери пакет ZIP размером до 8 МиБ.');
    const form = new FormData();
    form.append('password', uploadPassword.value);
    form.append('package', uploadFile.value);
    busy.value = true;
    uploadProgress.value = 'Загружаем и проверяем пакет…';
    let accepted = false;
    try {
      const d = await api<{ job: Job }>('update', 'POST', form, true);
      accepted = true;
      uploadPassword.value = '';
      await waitForPanelUpdate(d.job, (m) => (uploadProgress.value = m));
    } finally {
      uploadPassword.value = '';
      if (epoch === sessionGeneration()) busy.value = false;
      if (!accepted) uploadProgress.value = 'Пакет не установлен.';
    }
  }
  async function refresh(tab = 'panel') {
    if (tab === 'panel') await load();
    else if (tab === 'core') await core.value?.refresh();
  }
  async function check() {
    github.value = await api<ReleaseStatus>('github-check', 'POST', {});
  }
  registerUnsaved(() => channelChanged.value);
  return {
    channel,
    channelPassword,
    channelChanged,
    saveChannel,
    returnStable,
    check,
    github,
    stamp,
    run,
    install,
    githubPassword,
    pending,
    busy,
    latest,
    githubProgress,
    upload,
    uploadFile,
    uploadPassword,
    uploadProgress,
    refresh,
  };
}
