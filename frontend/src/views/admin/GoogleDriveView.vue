<template>
  <AppLayout>
    <div class="w-full min-w-0 space-y-6 pb-8">
      <header
        class="page-header mb-0 rounded-3xl bg-white p-5 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 class="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
              <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-blue-50 text-blue-500 dark:bg-blue-900/30 dark:text-blue-400">
                <Icon name="cloud" size="sm" />
              </span>
              {{ t('admin.googleDrive.title') }}
            </h1>
            <p class="page-description mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.googleDrive.description') }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadStatus">
            <Icon name="refresh" size="sm" class="mr-1" />
            {{ t('common.refresh') }}
          </button>
        </div>
        <div
          v-if="driveStatus && !driveStatus.enabled"
          class="mt-4 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
        >
          {{ t('admin.googleDrive.disabled') }}: {{ driveStatus.reason }}
        </div>
      </header>

      <section class="card p-5">
        <h2 class="mb-3 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.googleDrive.users') }}</h2>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <th class="py-2 pr-4">ID</th>
                <th class="py-2 pr-4">{{ t('admin.googleDrive.userEmail') }}</th>
                <th class="py-2 pr-4">{{ t('admin.googleDrive.status') }}</th>
                <th class="py-2 pr-4">{{ t('admin.googleDrive.updatedAt') }}</th>
                <th class="py-2 text-right">{{ t('admin.googleDrive.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="user in users"
                :key="user.user_id"
                class="border-b border-gray-50 dark:border-dark-700/60"
                :class="selected?.user_id === user.user_id ? 'bg-blue-50/60 dark:bg-blue-900/10' : ''"
              >
                <td class="py-2 pr-4 text-gray-500">{{ user.user_id }}</td>
                <td class="py-2 pr-4 font-medium text-gray-900 dark:text-white">{{ user.email }}</td>
                <td class="py-2 pr-4">
                  <span
                    class="inline-flex rounded-md px-2 py-0.5 text-xs font-medium"
                    :class="user.connected ? 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
                  >
                    {{ user.connected ? t('admin.googleDrive.connected') : t('admin.googleDrive.notConnected') }}
                  </span>
                </td>
                <td class="py-2 pr-4 text-gray-500">{{ user.updated_at ? formatTime(user.updated_at) : '-' }}</td>
                <td class="space-x-2 py-2 text-right">
                  <button v-if="user.connected" type="button" class="btn btn-primary btn-sm" @click="openUser(user)">
                    {{ t('admin.googleDrive.browse') }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="!driveStatus?.enabled || busy"
                    @click="authorize(user)"
                  >
                    {{ user.connected ? t('admin.googleDrive.reauthorize') : t('admin.googleDrive.authorize') }}
                  </button>
                  <button
                    v-if="user.connected"
                    type="button"
                    class="btn btn-danger btn-sm"
                    :disabled="busy"
                    @click="pendingDisconnect = user"
                  >
                    {{ t('admin.googleDrive.disconnect') }}
                  </button>
                </td>
              </tr>
              <tr v-if="!users.length && !loading">
                <td colspan="5" class="py-6 text-center text-gray-500">{{ t('admin.googleDrive.noUsers') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="authUrl" class="mt-3 break-all text-xs text-gray-500">
          {{ t('admin.googleDrive.authHint') }}
          <a :href="authUrl" target="_blank" rel="noopener" class="text-blue-600 underline">{{ t('admin.googleDrive.openAuth') }}</a>
        </p>
      </section>

      <section v-if="selected" class="card p-5">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ selected.email }}</h2>
            <p v-if="overviewData" class="mt-1 text-xs text-gray-500">
              {{ t('admin.googleDrive.googleAccount') }}: {{ overviewData.google_account || '-' }} ·
              {{ t('admin.googleDrive.storage') }}: {{ formatBytes(overviewData.usage) }} /
              {{ overviewData.limit == null ? t('admin.googleDrive.unlimited') : formatBytes(overviewData.limit) }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input v-model="newFolder" class="input h-9 w-44" :placeholder="t('admin.googleDrive.folderName')" @keyup.enter="createFolder" />
            <button type="button" class="btn btn-secondary btn-sm" :disabled="!newFolder.trim() || busy" @click="createFolder">
              <Icon name="plus" size="sm" class="mr-1" />{{ t('admin.googleDrive.newFolder') }}
            </button>
            <label class="btn btn-primary btn-sm cursor-pointer" :class="busy ? 'pointer-events-none opacity-60' : ''">
              <Icon name="upload" size="sm" class="mr-1" />{{ t('admin.googleDrive.upload') }}
              <input type="file" class="hidden" multiple @change="uploadFiles" />
            </label>
          </div>
        </div>

        <nav class="mb-3 flex flex-wrap items-center gap-1 text-sm">
          <button type="button" class="text-blue-600 hover:underline" @click="go('/')">{{ t('admin.googleDrive.root') }}</button>
          <template v-for="crumb in crumbs" :key="crumb.path">
            <span class="text-gray-400">/</span>
            <button type="button" class="text-blue-600 hover:underline" @click="go(crumb.path)">{{ crumb.name }}</button>
          </template>
        </nav>

        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <th class="py-2 pr-4">{{ t('admin.googleDrive.name') }}</th>
                <th class="py-2 pr-4">{{ t('admin.googleDrive.size') }}</th>
                <th class="py-2 pr-4">{{ t('admin.googleDrive.modified') }}</th>
                <th class="py-2 text-right">{{ t('admin.googleDrive.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="currentPath !== '/'" class="border-b border-gray-50 dark:border-dark-700/60">
                <td colspan="4" class="py-2">
                  <button type="button" class="flex items-center gap-1 text-blue-600 hover:underline" @click="go(parentPath)">
                    <Icon name="arrowLeft" size="sm" />..
                  </button>
                </td>
              </tr>
              <tr v-for="item in entries" :key="item.path" class="border-b border-gray-50 dark:border-dark-700/60">
                <td class="py-2 pr-4">
                  <button
                    v-if="item.is_dir"
                    type="button"
                    class="flex items-center gap-2 font-medium text-blue-600 hover:underline"
                    @click="go(item.path)"
                  >
                    <Icon name="inbox" size="sm" />{{ item.name }}
                  </button>
                  <span v-else class="flex items-center gap-2 text-gray-900 dark:text-white">
                    <Icon name="document" size="sm" class="text-gray-400" />{{ item.name }}
                  </span>
                </td>
                <td class="py-2 pr-4 text-gray-500">{{ item.is_dir ? '-' : formatBytes(item.size) }}</td>
                <td class="py-2 pr-4 text-gray-500">{{ formatTime(item.modified) }}</td>
                <td class="space-x-2 py-2 text-right">
                  <button v-if="!item.is_dir" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="downloadFile(item)">
                    <Icon name="download" size="sm" />
                  </button>
                  <button type="button" class="btn btn-danger btn-sm" :disabled="busy" @click="pendingDelete = item">
                    <Icon name="trash" size="sm" />
                  </button>
                </td>
              </tr>
              <tr v-if="!entries.length && !listing">
                <td colspan="4" class="py-6 text-center text-gray-500">{{ t('admin.googleDrive.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card space-y-2 p-5 text-sm text-gray-700 dark:text-gray-300">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.googleDrive.webdavTitle') }}</h2>
        <p>{{ t('admin.googleDrive.webdavIntro') }}</p>
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-gray-500">{{ t('admin.googleDrive.webdavUrl') }}:</span>
          <code class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">{{ webdavUrl }}</code>
          <button type="button" class="btn btn-secondary btn-sm" @click="copyWebdavUrl"><Icon name="copy" size="sm" /></button>
        </div>
        <p><span class="text-gray-500">{{ t('admin.googleDrive.webdavUser') }}:</span> {{ t('admin.googleDrive.webdavUserValue') }}</p>
        <p><span class="text-gray-500">{{ t('admin.googleDrive.webdavPassword') }}:</span> {{ t('admin.googleDrive.webdavPasswordValue') }}</p>
        <p class="text-xs text-gray-500">
          {{ t('admin.googleDrive.scopeNote', { size: formatBytes(driveStatus?.max_bytes ?? 0) }) }}
        </p>
      </section>
    </div>

    <ConfirmDialog
      :show="!!pendingDisconnect"
      :title="t('admin.googleDrive.disconnect')"
      :message="t('admin.googleDrive.confirmDisconnect', { email: pendingDisconnect?.email ?? '' })"
      danger
      @confirm="confirmDisconnect"
      @cancel="pendingDisconnect = null"
    />
    <ConfirmDialog
      :show="!!pendingDelete"
      :title="t('common.delete')"
      :message="t('admin.googleDrive.confirmDelete', { name: pendingDelete?.name ?? '' })"
      danger
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import driveAPI from '@/api/admin/googleDrive'
import type { DriveEntry, DriveOverview, DriveStatus, DriveUserStatus } from '@/api/admin/googleDrive'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const driveStatus = ref<DriveStatus | null>(null)
const loading = ref(false)
const listing = ref(false)
const busy = ref(false)
const authUrl = ref('')
const selected = ref<DriveUserStatus | null>(null)
const overviewData = ref<DriveOverview | null>(null)
const entries = ref<DriveEntry[]>([])
const currentPath = ref('/')
const newFolder = ref('')
const pendingDisconnect = ref<DriveUserStatus | null>(null)
const pendingDelete = ref<DriveEntry | null>(null)

const users = computed(() => driveStatus.value?.users ?? [])
const webdavUrl = computed(() => `${window.location.origin}/webdav/`)
const crumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  return parts.map((name, i) => ({ name, path: '/' + parts.slice(0, i + 1).join('/') }))
})
const parentPath = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  return '/' + parts.join('/')
})

function showError(err: unknown) {
  appStore.showError(extractApiErrorMessage(err, t('admin.googleDrive.requestFailed')))
}

function joinPath(dir: string, name: string) {
  return dir === '/' ? `/${name}` : `${dir}/${name}`
}

function formatBytes(value: number | null | undefined) {
  if (value == null) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = value
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? '-' : date.toLocaleString()
}

async function loadStatus() {
  loading.value = true
  try {
    driveStatus.value = await driveAPI.status()
    if (selected.value) {
      const fresh = users.value.find((u) => u.user_id === selected.value?.user_id)
      selected.value = fresh?.connected ? fresh : null
    }
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

async function loadEntries() {
  if (!selected.value) return
  listing.value = true
  try {
    entries.value = await driveAPI.list(selected.value.user_id, currentPath.value)
  } catch (err) {
    entries.value = []
    showError(err)
  } finally {
    listing.value = false
  }
}

async function openUser(user: DriveUserStatus) {
  selected.value = user
  currentPath.value = '/'
  overviewData.value = null
  entries.value = []
  driveAPI
    .overview(user.user_id)
    .then((data) => {
      if (selected.value?.user_id === user.user_id) overviewData.value = data
    })
    .catch(showError)
  await loadEntries()
}

function go(path: string) {
  currentPath.value = path || '/'
  loadEntries()
}

async function authorize(user: DriveUserStatus) {
  busy.value = true
  try {
    authUrl.value = await driveAPI.authorize(user.user_id)
    window.open(authUrl.value, '_blank', 'noopener')
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

async function confirmDisconnect() {
  const user = pendingDisconnect.value
  pendingDisconnect.value = null
  if (!user) return
  busy.value = true
  try {
    await driveAPI.disconnect(user.user_id)
    if (selected.value?.user_id === user.user_id) selected.value = null
    appStore.showSuccess(t('admin.googleDrive.disconnected'))
    await loadStatus()
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

async function createFolder() {
  const name = newFolder.value.trim()
  if (!selected.value || !name) return
  busy.value = true
  try {
    await driveAPI.mkdir(selected.value.user_id, joinPath(currentPath.value, name))
    newFolder.value = ''
    await loadEntries()
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

async function uploadFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (!selected.value || !files.length) return
  busy.value = true
  try {
    for (const file of files) {
      await driveAPI.upload(selected.value.user_id, currentPath.value, file)
    }
    appStore.showSuccess(t('admin.googleDrive.uploaded', { count: files.length }))
    await loadEntries()
  } catch (err) {
    showError(err)
    await loadEntries()
  } finally {
    busy.value = false
  }
}

async function downloadFile(item: DriveEntry) {
  if (!selected.value) return
  busy.value = true
  try {
    const blob = await driveAPI.download(selected.value.user_id, item.path)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = item.name
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

async function confirmDelete() {
  const item = pendingDelete.value
  pendingDelete.value = null
  if (!selected.value || !item) return
  busy.value = true
  try {
    await driveAPI.remove(selected.value.user_id, item.path)
    await loadEntries()
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

async function copyWebdavUrl() {
  try {
    await navigator.clipboard.writeText(webdavUrl.value)
    appStore.showSuccess(t('common.copied'))
  } catch {
    appStore.showError(t('admin.googleDrive.copyFailed'))
  }
}

onMounted(loadStatus)
</script>
