import { computed, ref, type Ref } from 'vue';
import { api } from '../api';
import type { ReleaseStatus, Job } from '../types';
import { busy, cleanDraft, waitForPanelUpdate } from '../store';
import { stamp, useAction } from './ui';

/** Signed panel updates. The caller owns the child Xray view; only its refresh contract crosses this boundary.
 * Create once per mounted view; mutable form state must not become a module singleton. */
export function useUpdates(core: Ref<{ refresh: () => Promise<void> } | undefined>) {
  const github = ref<ReleaseStatus>({}),
    githubPassword = ref(''),
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
  }
  async function install() {
    cleanDraft();
    const version = latest.value;
    if (!version) throw new Error('Сначала проверь обновления.');
    busy.value = true;
    githubProgress.value = 'Загружаем выпуск и проверяем подпись…';
    try {
      const d = await api<{ job: Job }>('github-install', 'POST', {
        password: githubPassword.value,
        version,
      });
      githubPassword.value = '';
      await waitForPanelUpdate(d.job, (m) => (githubProgress.value = m));
    } finally {
      githubPassword.value = '';
      busy.value = false;
    }
  }
  async function upload() {
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
      busy.value = false;
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
  return {
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
