<template>
  <AppLayout>
    <div class="space-y-6 pb-8">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-bold text-gray-900 dark:text-white">{{ title }}</h1>
          <p class="mt-2 max-w-2xl text-sm text-gray-500 dark:text-gray-400">{{ t('compute.resourceGroupScope') }}</p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary btn-sm" :disabled="loading || saving" @click="load">{{ t('common.refresh') }}</button>
          <button class="btn btn-primary btn-sm" :disabled="loading || saving || !loaded" @click="edit()">{{ t('compute.createGroup') }}</button>
        </div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <div class="flex flex-wrap items-center justify-between gap-3 text-sm text-gray-500">
        <p v-if="loaded">{{ t('compute.groupSummary', {groups: activeGroups.length, accounts: accounts.length, ungrouped: ungrouped.length}) }}
          <RouterLink :to="accountPath" class="ml-2 text-primary-600 hover:underline">{{ t('compute.manageAccounts') }}</RouterLink>
        </p>
        <label class="flex items-center gap-2"><input v-model="showArchived" type="checkbox" class="rounded" />{{ t('compute.showArchived') }}</label>
      </div>
      <section class="card overflow-hidden" :aria-busy="loading">
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="border-b border-gray-200 bg-gray-50 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800">
              <tr><th class="px-5 py-3">{{ t('compute.groupName') }}</th><th class="px-5 py-3">{{ t('compute.groupMembers') }}</th><th class="px-5 py-3">{{ t('compute.status') }}</th><th class="px-5 py-3 text-right">{{ t('compute.actions') }}</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="group in visibleGroups" :key="group.id" class="hover:bg-gray-50 dark:hover:bg-dark-800/50">
                <td class="max-w-xs px-5 py-4"><p class="break-words font-medium text-gray-900 dark:text-white">{{ group.name }}</p><p class="mt-1 break-words text-xs text-gray-500">{{ group.description || '—' }}</p></td>
                <td class="px-5 py-4"><p>{{ t('compute.memberSummary', {total: group.account_ids.length, enabled: members(group).filter(a => a.enabled).length}) }}</p><p class="mt-1 max-w-md break-words text-xs text-gray-500">{{ members(group).map(a => a.email).join(', ') || t('compute.noGroupMembers') }}</p></td>
                <td class="px-5 py-4"><span class="rounded-full px-2 py-1 text-xs" :class="group.archived ? 'bg-gray-100 text-gray-500 dark:bg-dark-700' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">{{ t(group.archived ? 'compute.archivedGroup' : 'compute.activeGroup') }}</span></td>
                <td class="whitespace-nowrap px-5 py-4 text-right"><button class="btn btn-secondary btn-sm" :disabled="loading || saving" @click="edit(group)">{{ t('compute.editGroup') }}</button></td>
              </tr>
              <tr v-if="!visibleGroups.length"><td colspan="4" class="px-5 py-12 text-center text-gray-500">{{ loading ? t('common.loading') : t('compute.noGroups') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
      <BaseDialog :show="dialog" :title="editingID ? t('compute.editGroup') : t('compute.createGroup')" width="wide" @close="close">
        <form id="resource-group-form" class="space-y-5" @submit.prevent="save">
          <p v-if="saveError" role="alert" class="text-sm text-red-600">{{ saveError }}</p>
          <label class="block text-sm font-medium">{{ t('compute.groupName') }}<input v-model="form.name" required maxlength="100" class="input mt-2 w-full" :disabled="saving" /></label>
          <label class="block text-sm font-medium">{{ t('compute.groupDescription') }}<textarea v-model="form.description" maxlength="1000" rows="2" class="input mt-2 w-full" :disabled="saving" /></label>
          <fieldset :disabled="saving"><legend class="mb-2 text-sm font-medium">{{ t('compute.groupMembers') }} · {{ form.account_ids.length }}</legend>
            <p class="mb-3 text-xs text-gray-500">{{ t('compute.memberHint') }}</p>
            <div class="max-h-64 overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <label v-for="account in accounts" :key="account.id" class="flex cursor-pointer items-center gap-3 border-b border-gray-100 px-4 py-3 last:border-0 dark:border-dark-700">
                <input v-model="form.account_ids" type="checkbox" :value="account.id" class="rounded" />
                <span class="min-w-0 flex-1 break-all text-sm">{{ account.email }} <span class="text-xs text-gray-400">#{{ account.id }}</span></span>
                <span class="text-xs text-gray-500">{{ t(account.enabled ? 'compute.enabled' : 'compute.disabled') }}</span>
              </label>
              <p v-if="!accounts.length" class="p-4 text-sm text-gray-500">{{ t('compute.noAccountsForGroup') }}</p>
            </div>
          </fieldset>
          <label v-if="editingID" class="flex items-start gap-2 text-sm"><input v-model="form.archived" type="checkbox" :disabled="saving" class="mt-1 rounded" /><span>{{ t('compute.archivedGroup') }}<span class="mt-1 block text-xs text-gray-500">{{ t('compute.archiveHint') }}</span></span></label>
        </form>
        <template #footer><button class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button><button type="submit" form="resource-group-form" class="btn btn-primary" :disabled="saving || !form.name.trim()">{{ saving ? t('common.loading') : t('common.save') }}</button></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import api from '@/api/client'
interface Account { id: number; email: string; enabled: boolean }
interface Group { id: number; kind: string; name: string; description: string; version: number; archived: boolean; account_ids: number[] }
const { t } = useI18n()
const route = useRoute()
const kind = computed(() => route.path.endsWith('/storage') ? 'storage' : 'compute')
const title = computed(() => t(kind.value === 'storage' ? 'compute.storageGroups' : 'compute.computeGroups'))
const accountPath = computed(() => kind.value === 'storage' ? '/admin/drive' : '/admin/compute')
const groups = ref<Group[]>([]), accounts = ref<Account[]>([])
const loading = ref(false), loaded = ref(false), saving = ref(false), showArchived = ref(false), dialog = ref(false)
const error = ref(''), saveError = ref(''), editingID = ref(0)
const emptyForm = () => ({name: '', description: '', version: 0, archived: false, account_ids: [] as number[]})
const form = ref(emptyForm())
const activeGroups = computed(() => groups.value.filter(g => !g.archived))
const visibleGroups = computed(() => groups.value.filter(g => showArchived.value || !g.archived))
const ungrouped = computed(() => accounts.value.filter(a => !activeGroups.value.some(g => g.account_ids.includes(a.id))))
const members = (g: Group) => accounts.value.filter(a => g.account_ids.includes(a.id))
const message = (e: unknown) => (e as {message?: string})?.message || t('compute.groupRequestFailed')
let generation = 0
async function load() {
  const request = ++generation, currentKind = kind.value
  loading.value = true; error.value = ''
  try {
    const {data} = await api.get<{groups: Group[]; accounts: Account[]}>(`/admin/resource-groups/${currentKind}`)
    if (request !== generation) return
    groups.value = data.groups; accounts.value = data.accounts; loaded.value = true
  } catch (e) { if (request === generation) error.value = message(e) }
  finally { if (request === generation) loading.value = false }
}
function edit(g?: Group) {
  editingID.value = g?.id || 0
  form.value = g ? {name: g.name, description: g.description, version: g.version, archived: g.archived, account_ids: [...g.account_ids]} : emptyForm()
  saveError.value = ''; dialog.value = true
}
function close() { if (!saving.value) dialog.value = false }
async function save() {
  if (saving.value) return
  saving.value = true; saveError.value = ''
  const currentKind = kind.value
  try {
    const path = `/admin/resource-groups/${currentKind}`
    if (editingID.value) await api.put(`${path}/${editingID.value}`, form.value)
    else await api.post(path, form.value)
    if (kind.value === currentKind) { dialog.value = false; await load() }
  } catch (e) { if (kind.value === currentKind) saveError.value = message(e) }
  finally { saving.value = false }
}
watch(kind, () => { dialog.value = false; loaded.value = false; groups.value = []; accounts.value = []; showArchived.value = false; void load() }, {immediate: true})
</script>
