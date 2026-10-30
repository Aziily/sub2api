<template>
  <AppLayout>
    <div class="w-full min-w-0 space-y-6 pb-8">
      <header class="flex flex-col gap-3">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 class="flex items-center gap-2 text-xl font-bold text-gray-900 dark:text-white">
              <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
                <StorageIcon class="h-5 w-5" />
              </span>
              {{ t('drive.title') }}
            </h1>
            <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('drive.description') }}</p>
          </div>
          <div class="flex gap-2">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="load">
              <Icon name="refresh" size="sm" class="mr-1" />{{ t('common.refresh') }}
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="!overview?.enabled" data-testid="drive-browse" @click="drawerOpen = true">
              <Icon name="folder" size="sm" class="mr-1" />{{ t('drive.browse') }}
            </button>
          </div>
        </div>
        <div v-if="overview && !overview.enabled" class="mt-4 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
          {{ t('drive.unavailable') }}
        </div>
      </header>

      <section class="card p-5">
        <div class="mb-3 flex flex-wrap items-end justify-between gap-2">
          <div>
            <p class="text-xs text-gray-500">{{ t('drive.used') }}</p>
            <p class="text-2xl font-bold text-gray-900 dark:text-white" data-testid="drive-used">{{ formatBytes(overview?.used_bytes) }}</p>
          </div>
          <p class="text-sm text-gray-500">
            <template v-if="overview?.quota_bytes != null">{{ t('drive.ofQuota', { quota: formatBytes(overview.quota_bytes) }) }}</template>
            <template v-else-if="overview">{{ t('drive.noQuota') }}</template>
          </p>
        </div>
        <div v-if="overview?.quota_bytes" class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
          <div class="h-full rounded-full bg-primary-500 transition-all" :style="{ width: usedPercent + '%' }" />
        </div>
        <dl class="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div>
            <dt class="text-xs text-gray-500">{{ t('drive.available') }}</dt>
            <dd class="mt-1 font-semibold text-gray-900 dark:text-white">
              {{ !overview ? '—' : overview.available_bytes == null ? t('drive.unlimited') : formatBytes(overview.available_bytes) }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">{{ t('drive.files') }}</dt>
            <dd class="mt-1 font-semibold text-gray-900 dark:text-white" data-testid="drive-file-count">{{ overview?.file_count ?? '—' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">{{ t('drive.folders') }}</dt>
            <dd class="mt-1 font-semibold text-gray-900 dark:text-white">{{ overview?.folder_count ?? '—' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">{{ t('drive.uploadingNow') }}</dt>
            <dd class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatBytes(overview?.reserved_bytes) }}</dd>
          </div>
        </dl>
      </section>

      <section class="card space-y-4 p-5 text-sm">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('drive.webdavTitle') }}</h2>
          <p class="mt-1 text-gray-500 dark:text-gray-400">{{ t('drive.webdavIntro') }}</p>
        </div>
        <dl class="grid gap-3 sm:grid-cols-[8rem_1fr]">
          <dt class="text-gray-500">{{ t('drive.webdavUrl') }}</dt>
          <dd class="flex min-w-0 items-center gap-2">
            <code class="truncate rounded bg-gray-100 px-2 py-1 dark:bg-dark-700" data-testid="drive-webdav-url">{{ webdavUrl }}</code>
            <button type="button" class="btn btn-ghost btn-sm" :title="t('common.copy')" @click="copy(webdavUrl)"><Icon name="copy" size="sm" /></button>
          </dd>
          <dt class="text-gray-500">{{ t('drive.webdavUser') }}</dt>
          <dd class="flex min-w-0 items-center gap-2">
            <code class="truncate rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">{{ email }}</code>
            <button type="button" class="btn btn-ghost btn-sm" :title="t('common.copy')" @click="copy(email)"><Icon name="copy" size="sm" /></button>
          </dd>
          <dt class="text-gray-500">{{ t('drive.webdavPassword') }}</dt>
          <dd>
            {{ t('drive.webdavPasswordValue') }}
            <RouterLink to="/keys" class="ml-1 text-primary-600 hover:underline dark:text-primary-400">{{ t('drive.manageKeys') }}</RouterLink>
          </dd>
        </dl>
        <div class="rounded-xl bg-gray-50 p-3 text-xs leading-relaxed text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
          <p>{{ t('drive.finderHint') }}</p>
          <p v-if="overview" class="mt-1">{{ t('drive.limitHint', { size: formatBytes(overview?.max_relay_bytes ?? 0) }) }}</p>
        </div>
      </section>
    </div>

    <DriveFileDrawer
      :show="drawerOpen"
      :client="myDrive"
      :title="t('drive.title')"
      :subtitle="email"
      @close="drawerOpen = false"
      @changed="load"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatBytes, myDrive, type DriveOverview } from '@/api/drive'
import AppLayout from '@/components/layout/AppLayout.vue'
import DriveFileDrawer from '@/components/drive/DriveFileDrawer.vue'
import StorageIcon from '@/components/icons/StorageIcon.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const overview = ref<DriveOverview | null>(null)
const loading = ref(false)
const drawerOpen = ref(false)

const email = computed(() => authStore.user?.email ?? '')
const webdavUrl = computed(() => `${window.location.origin}/webdav/`)
const usedPercent = computed(() => {
  const quota = overview.value?.quota_bytes
  if (!quota) return 0
  return Math.min(100, ((overview.value?.used_bytes ?? 0) / quota) * 100)
})

async function load() {
  loading.value = true
  try {
    overview.value = (await myDrive.overview()).overview
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('drive.requestFailed')))
  } finally {
    loading.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    appStore.showSuccess(t('common.copied'))
  } catch {
    appStore.showError(t('drive.copyFailed'))
  }
}

onMounted(load)
</script>
