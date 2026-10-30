<template>
  <AppLayout>
    <div class="w-full min-w-0 space-y-6 pb-8">
      <header class="flex flex-col gap-3">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 class="text-xl font-bold text-gray-900 dark:text-white">{{ t('admin.driveStorage.title') }}</h1>
            <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.driveStorage.description') }}</p>
          </div>
          <div class="flex gap-2">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="load">
              <Icon name="refresh" size="sm" class="mr-1" />{{ t('common.refresh') }}
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="!state?.enabled || busy" @click="authorize(0)">
              <Icon name="plus" size="sm" class="mr-1" />{{ t('admin.driveStorage.addAccount') }}
            </button>
          </div>
        </div>
        <p v-if="authUrl" class="mt-3 text-xs text-gray-500">
          {{ t('admin.driveStorage.authHint') }}
          <a :href="authUrl" target="_blank" rel="noopener" class="text-primary-600 underline">{{ t('admin.driveStorage.openAuth') }}</a>
        </p>
      </header>



      <section class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div v-for="card in poolCards" :key="card.label" class="card p-4">
          <p class="text-xs text-gray-500">{{ card.label }}</p>
          <p class="mt-1 text-xl font-bold text-gray-900 dark:text-white">{{ card.value }}</p>
          <p v-if="card.hint" class="mt-0.5 text-xs text-gray-400">{{ card.hint }}</p>
        </div>
      </section>

      <details class="card p-5" data-testid="drive-checks">
        <summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.driveStorage.checks') }}</summary>
        <ul class="mt-4 grid gap-2 sm:grid-cols-2">
          <li v-for="check in state?.checks ?? []" :key="check.key" class="flex items-start gap-3 rounded-xl border border-gray-100 p-3 dark:border-dark-700">
            <Icon :name="check.ok ? 'checkCircle' : 'exclamationTriangle'" size="md" :class="check.ok ? 'text-emerald-500' : 'text-amber-500'" />
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-900 dark:text-white">{{ checkLabel(check.key) }}</p>
              <p class="break-all text-xs text-gray-500">{{ checkDetail(check) }}</p>
            </div>
          </li>
        </ul>
      </details>

      <section class="card p-5">
        <h2 class="mb-1 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.driveStorage.accounts') }}</h2>
        <p class="mb-3 text-xs text-gray-500">{{ t('admin.driveStorage.accountsHint') }}</p>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-700">
                <th class="py-2 pr-4">{{ t('admin.driveStorage.googleAccount') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.status') }}</th>
                <th class="min-w-[12rem] py-2 pr-4">{{ t('admin.driveStorage.space') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.poolFiles') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.checkedAt') }}</th>
                <th class="py-2 text-right">{{ t('admin.driveStorage.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in accounts" :key="a.id" class="border-b border-gray-50 align-top dark:border-dark-700/60">
                <td class="py-3 pr-4">
                  <p class="font-medium text-gray-900 dark:text-white">{{ a.google_email }}</p>
                  <p class="text-xs text-gray-400">#{{ a.id }}</p>
                </td>
                <td class="py-3 pr-4">
                  <span class="inline-flex rounded-md px-2 py-0.5 text-xs font-medium" :class="accountBadge(a).cls">{{ accountBadge(a).text }}</span>
                  <p v-if="a.last_error" class="mt-1 max-w-[16rem] break-words text-xs text-red-500">{{ a.last_error }}</p>
                </td>
                <td class="py-3 pr-4">
                  <div v-if="a.quota_limit" class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                    <div class="h-full rounded-full" :class="percent(a) > 90 ? 'bg-red-500' : 'bg-primary-500'" :style="{ width: percent(a) + '%' }" />
                  </div>
                  <p class="mt-1 text-xs text-gray-500">
                    {{ formatBytes(a.quota_usage) }} / {{ a.quota_limit == null ? t('admin.driveStorage.unlimited') : formatBytes(a.quota_limit) }}
                  </p>
                  <p v-if="a.reserved_bytes" class="text-xs text-gray-400">{{ t('admin.driveStorage.reserved', { size: formatBytes(a.reserved_bytes) }) }}</p>
                </td>
                <td class="py-3 pr-4 text-gray-600 dark:text-gray-300">{{ a.file_count }} · {{ formatBytes(a.stored_bytes) }}</td>
                <td class="py-3 pr-4 text-xs text-gray-500">{{ a.quota_checked_at ? formatTime(a.quota_checked_at) : '-' }}</td>
                <td class="whitespace-nowrap py-3 text-right">
                  <button type="button" class="btn btn-ghost btn-sm" :title="t('admin.driveStorage.refreshQuota')" :disabled="busy" @click="refreshAccount(a)">
                    <Icon name="refresh" size="sm" />
                  </button>
                  <button type="button" class="btn btn-ghost btn-sm" :title="t('admin.driveStorage.reauthorize')" :disabled="!state?.enabled || busy" @click="authorize(a.id)">
                    <Icon name="key" size="sm" />
                  </button>
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="toggleAccount(a)">
                    {{ a.enabled ? t('admin.driveStorage.disable') : t('admin.driveStorage.enable') }}
                  </button>
                  <button type="button" class="btn btn-ghost btn-sm text-red-500" :title="t('admin.driveStorage.remove')" :disabled="busy || a.file_count > 0" @click="pendingRemove = a">
                    <Icon name="trash" size="sm" />
                  </button>
                </td>
              </tr>
              <tr v-if="!accounts.length && !loading">
                <td colspan="6" class="py-8 text-center text-gray-500">{{ t('admin.driveStorage.noAccounts') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card p-5">
        <h2 class="mb-1 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.driveStorage.users') }}</h2>
        <p class="mb-3 text-xs text-gray-500">{{ t('admin.driveStorage.usersHint') }}</p>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-700">
                <th class="py-2 pr-4">{{ t('admin.driveStorage.user') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.used') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.items') }}</th>
                <th class="py-2 pr-4">{{ t('admin.driveStorage.quota') }}</th>
                <th class="py-2 text-right">{{ t('admin.driveStorage.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.user_id" class="border-b border-gray-50 dark:border-dark-700/60">
                <td class="py-2.5 pr-4">
                  <p class="font-medium text-gray-900 dark:text-white">{{ u.email }}</p>
                  <p class="text-xs text-gray-400">ID {{ u.user_id }}</p>
                </td>
                <td class="py-2.5 pr-4">{{ formatBytes(u.used_bytes) }}</td>
                <td class="py-2.5 pr-4 text-gray-600 dark:text-gray-300">{{ t('admin.driveStorage.itemCount', { files: u.file_count, folders: u.folder_count }) }}</td>
                <td class="py-2.5 pr-4">
                  <div v-if="editingQuota === u.user_id" class="flex items-center gap-1">
                    <input v-model="quotaInput" class="input h-8 w-24" inputmode="decimal" :placeholder="t('admin.driveStorage.quotaPlaceholder')" @keyup.enter="saveQuota(u.user_id)" />
                    <span class="text-xs text-gray-500">GB</span>
                    <button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="saveQuota(u.user_id)">{{ t('common.save') }}</button>
                    <button type="button" class="btn btn-ghost btn-sm" @click="editingQuota = null"><Icon name="x" size="sm" /></button>
                  </div>
                  <button v-else type="button" class="text-primary-600 hover:underline dark:text-primary-400" @click="editQuota(u)">
                    {{ u.quota_bytes == null ? t('admin.driveStorage.noQuota') : formatBytes(u.quota_bytes) }}
                  </button>
                </td>
                <td class="py-2.5 text-right">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="!state?.enabled" @click="browse(u.user_id, u.email)">
                    <Icon name="folder" size="sm" class="mr-1" />{{ t('drive.browse') }}
                  </button>
                </td>
              </tr>
              <tr v-if="!users.length && !loading">
                <td colspan="5" class="py-8 text-center text-gray-500">{{ t('admin.driveStorage.noUsers') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <DriveFileDrawer
      :show="!!browsing"
      :client="browsing?.client ?? null"
      :title="t('admin.driveStorage.userFiles')"
      :subtitle="browsing?.email"
      @close="browsing = null"
      @changed="load"
    />
    <ConfirmDialog
      :show="!!pendingRemove"
      :title="t('admin.driveStorage.remove')"
      :message="t('admin.driveStorage.confirmRemove', { email: pendingRemove?.google_email ?? '' })"
      danger
      @confirm="confirmRemove"
      @cancel="pendingRemove = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import driveAdmin, { type DriveAdminStatus, type DriveConfigCheck, type DriveStorageAccount, type DriveUserUsage } from '@/api/admin/drive'
import { formatBytes, type DriveClient } from '@/api/drive'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DriveFileDrawer from '@/components/drive/DriveFileDrawer.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const state = ref<DriveAdminStatus | null>(null)
const loading = ref(false)
const busy = ref(false)
const authUrl = ref('')
const pendingRemove = ref<DriveStorageAccount | null>(null)
const browsing = ref<{ client: DriveClient; email: string } | null>(null)
const editingQuota = ref<number | null>(null)
const quotaInput = ref('')

const accounts = computed(() => state.value?.accounts ?? [])
const users = computed(() => state.value?.users ?? [])
const poolCards = computed(() => {
  const p = state.value?.pool
  return [
    { label: t('admin.driveStorage.poolAccounts'), value: p ? `${p.usable_accounts} / ${p.accounts}` : '-', hint: t('admin.driveStorage.poolAccountsHint') },
    { label: t('admin.driveStorage.poolCapacity'), value: p ? (p.unlimited ? t('admin.driveStorage.unlimited') : formatBytes(p.capacity_bytes)) : '-', hint: '' },
    { label: t('admin.driveStorage.poolFree'), value: p ? (p.unlimited ? t('admin.driveStorage.unlimited') : formatBytes(p.free_bytes)) : '-', hint: t('admin.driveStorage.poolFreeHint') },
    { label: t('admin.driveStorage.poolStored'), value: p ? formatBytes(p.stored_bytes) : '-', hint: p ? t('admin.driveStorage.fileCount', { count: p.file_count }) : '' }
  ]
})

function showError(err: unknown) {
  appStore.showError(extractApiErrorMessage(err, t('drive.requestFailed')))
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString()
}

function checkLabel(key: string) {
  const known = ['encryption', 'oauth_client', 'redirect_uri', 'storage']
  return known.includes(key) ? t(`admin.driveStorage.check.${key}`) : key
}

function checkDetail(check: DriveConfigCheck) {
  switch (check.key) {
    case 'encryption':
      return check.ok ? t('admin.driveStorage.check.encryptionOk') : t('admin.driveStorage.check.encryptionMissing')
    case 'oauth_client':
      return check.ok ? t('admin.driveStorage.check.oauthOk') : check.detail
    case 'redirect_uri':
      return check.ok ? check.detail : t('admin.driveStorage.check.redirectBad', { uri: check.detail || '-' })
    case 'storage':
      return t('admin.driveStorage.check.storageDetail', { detail: check.detail })
    default:
      return check.detail
  }
}

function accountBadge(a: DriveStorageAccount) {
  if (!a.enabled) return { text: t('admin.driveStorage.disabled'), cls: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300' }
  if (a.last_error) return { text: t('admin.driveStorage.error'), cls: 'bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300' }
  if (!a.quota_checked_at) return { text: t('admin.driveStorage.unchecked'), cls: 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' }
  return { text: t('admin.driveStorage.healthy'), cls: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' }
}

function percent(a: DriveStorageAccount) {
  if (!a.quota_limit) return 0
  return Math.min(100, (((a.quota_usage ?? 0) + a.reserved_bytes) / a.quota_limit) * 100)
}

async function load() {
  loading.value = true
  try {
    state.value = await driveAdmin.status()
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

async function act(action: () => Promise<unknown>, success?: string) {
  busy.value = true
  try {
    await action()
    if (success) appStore.showSuccess(success)
    await load()
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
  }
}

function authorize(accountId: number) {
  act(async () => {
    authUrl.value = await driveAdmin.authorize(accountId)
    window.open(authUrl.value, '_blank', 'noopener')
  })
}

function refreshAccount(a: DriveStorageAccount) {
  act(() => driveAdmin.refresh(a.id), t('admin.driveStorage.refreshed'))
}

function toggleAccount(a: DriveStorageAccount) {
  act(() => driveAdmin.setEnabled(a.id, !a.enabled))
}

function confirmRemove() {
  const a = pendingRemove.value
  pendingRemove.value = null
  if (a) act(() => driveAdmin.remove(a.id), t('admin.driveStorage.removed'))
}

function editQuota(u: DriveUserUsage) {
  editingQuota.value = u.user_id
  quotaInput.value = u.quota_bytes == null ? '' : String(+(u.quota_bytes / 1024 ** 3).toFixed(2))
}

function saveQuota(userId: number) {
  const raw = quotaInput.value.trim()
  const gb = Number(raw)
  if (raw !== '' && (!Number.isFinite(gb) || gb < 0)) {
    appStore.showError(t('admin.driveStorage.quotaInvalid'))
    return
  }
  editingQuota.value = null
  act(() => driveAdmin.setQuota(userId, raw === '' ? null : Math.round(gb * 1024 ** 3)))
}

function browse(userId: number, email: string) {
  browsing.value = { client: driveAdmin.userDrive(userId), email }
}

onMounted(load)
</script>
