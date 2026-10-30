<template>
  <section class="space-y-3" data-testid="resource-overview">
    <header class="flex flex-wrap items-center justify-between gap-2">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('compute.dashboardTitle') }}</h2>
        <p class="text-xs text-gray-500">{{ t(admin ? 'compute.dashboardAdminScope' : 'compute.dashboardUserScope') }}</p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
    </header>
    <div class="grid gap-4 xl:grid-cols-2">
      <article class="card p-5" data-testid="storage-summary" :aria-busy="busy">
        <header class="mb-4 flex items-center justify-between">
          <h3 class="flex items-center gap-2 font-semibold"><Icon name="database" size="md" class="text-blue-600" />{{ t('compute.navStorage') }}</h3>
          <RouterLink class="text-sm text-primary-600 hover:underline" :to="admin ? '/admin/drive' : '/drive'">{{ t('compute.dashboardOpen') }}</RouterLink>
        </header>
        <p v-if="storageError" role="alert" class="text-sm text-red-600">{{ t('compute.dashboardStorageError') }}</p>
        <p v-else-if="!storage" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <template v-else>
          <dl class="grid grid-cols-2 gap-x-5 gap-y-4 sm:grid-cols-3">
            <div v-for="item in storageStats" :key="item.label">
              <dt class="text-xs text-gray-500">{{ item.label }}</dt>
              <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</dd>
            </div>
          </dl>
          <p class="mt-4 border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700">
            {{ t(storage.enabled ? 'compute.dashboardStorageReady' : 'compute.dashboardStorageDisabled') }}
            <span v-if="admin"> · {{ t('compute.dashboardSnapshot') }} {{ stamp(storage.checkedAt) }}</span>
          </p>
        </template>
      </article>
      <article class="card p-5" data-testid="compute-summary" :aria-busy="busy">
        <header class="mb-4 flex items-center justify-between">
          <h3 class="flex items-center gap-2 font-semibold"><ComputeIcon class="h-5 w-5 text-purple-600" />{{ t('compute.navCompute') }}</h3>
          <RouterLink class="text-sm text-primary-600 hover:underline" :to="admin ? '/admin/compute' : '/compute'">{{ t('compute.dashboardOpen') }}</RouterLink>
        </header>
        <p v-if="computeError" role="alert" class="text-sm text-red-600">{{ t('compute.dashboardComputeError') }}</p>
        <p v-else-if="!compute" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <template v-else>
          <dl class="grid grid-cols-2 gap-x-5 gap-y-4 sm:grid-cols-3">
            <div v-for="item in computeStats" :key="item.label">
              <dt class="text-xs text-gray-500">{{ item.label }}</dt>
              <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</dd>
            </div>
          </dl>
          <p v-if="admin" class="mt-4 border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700">
            {{ t('compute.dashboardCUCoverage', { known: compute.cu_accounts_known ?? 0, total: compute.accounts?.length ?? 0 }) }} · {{ t('compute.dashboardSnapshot') }} {{ stamp(cuCheckedAt) }}
            <span v-if="cuRefreshFailed" class="text-amber-600"> · {{ t('compute.cuStale') }}</span>
          </p>
          <p class="mt-2 text-xs text-gray-500">{{ t(compute.compute_billing_enabled ? 'compute.billingEnabledHint' : 'compute.billingHint') }}</p>
        </template>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { status as adminDriveStatus } from '@/api/admin/drive'
import { myDrive, formatBytes } from '@/api/drive'
import Icon from '@/components/icons/Icon.vue'
import ComputeIcon from '@/components/icons/ComputeIcon.vue'

const props = defineProps<{ admin?: boolean }>()
const { t } = useI18n()
interface StorageSummary { enabled:boolean; used:number; reserved:number; files:number; capacity:number|null; free:number|null; usable?:number; total?:number; checkedAt?:string|null }
interface ComputeSummary {
  compute_units:number|null; platform_credits:number; cu_accounts_known?:number; compute_billing_enabled:boolean;
  accounts?:{consumption_checked_at?:string; consumption_error?:string; consumption?:{currentBalance?:number}}[];
  usage_summary:{total_jobs:number;succeeded_jobs:number;queued_jobs:number;running_jobs:number;failed_jobs:number;gpu_runtime_seconds:number;charged_cost:string;reserved_cost:string}
}
const storage = ref<StorageSummary|null>(null), compute = ref<ComputeSummary|null>(null)
const storageError = ref(false), computeError = ref(false), busy = ref(false)
function oldest(values:(string|null|undefined)[]) { const valid = values.filter((x):x is string => !!x).sort(); return valid[0] }
function stamp(value?:string|null) { return value ? new Date(value).toLocaleString() : t('compute.dashboardUnknown') }
function number(value:number|null|undefined, digits=0) { return value == null ? '—' : value.toLocaleString(undefined,{maximumFractionDigits:digits}) }
function money(value:string|number|undefined) { return value == null ? '—' : Number(value).toLocaleString(undefined,{minimumFractionDigits:2,maximumFractionDigits:6}) }
const cuCheckedAt = computed(() => oldest((compute.value?.accounts ?? []).filter(x => x.consumption?.currentBalance != null).map(x => x.consumption_checked_at)))
const cuRefreshFailed = computed(() => compute.value?.accounts?.some(x => !!x.consumption_error))
const storageStats = computed(() => {
  const s=storage.value; if (!s) return []
  return [
    {label:t('compute.dashboardStored'),value:formatBytes(s.used)},
    {label:t('compute.dashboardFiles'),value:number(s.files)},
    {label:t(props.admin ? 'compute.dashboardPoolFree' : 'compute.dashboardFree'),value:formatBytes(s.free)},
    {label:t(props.admin ? 'compute.dashboardCapacity' : 'compute.dashboardQuota'),value:s.capacity == null ? t('compute.dashboardUnknown') : formatBytes(s.capacity)},
    {label:t('compute.dashboardStorageReserved'),value:formatBytes(s.reserved)},
    ...(props.admin ? [{label:t('compute.dashboardStorageAccounts'),value:`${s.usable} / ${s.total}`}] : [])
  ]
})
const computeStats = computed(() => {
  const c=compute.value; if (!c) return []; const u=c.usage_summary
  return [
    {label:t(props.admin ? 'compute.googleCU' : 'compute.platformCredits'),value:number(props.admin ? c.compute_units : c.platform_credits,4)},
    {label:t('compute.dashboardRunning'),value:number(u.running_jobs)},
    {label:t('compute.dashboardQueued'),value:number(u.queued_jobs)},
    {label:t('compute.dashboardSucceeded'),value:number(u.succeeded_jobs)+' / '+number(u.total_jobs)},
    {label:t('compute.dashboardGPUHours'),value:number(u.gpu_runtime_seconds/3600,3)},
    {label:t('compute.dashboardFailed'),value:number(u.failed_jobs)},
    {label:t('compute.dashboardCharged'),value:money(u.charged_cost)},
    {label:t('compute.dashboardHeld'),value:money(u.reserved_cost)}
  ]
})
async function loadStorage() {
  try {
    if (props.admin) {
      const s=await adminDriveStatus(), p=s.pool
      storage.value={enabled:s.enabled && p.usable_accounts>0,used:p.stored_bytes,files:p.file_count,capacity:p.unlimited?null:p.capacity_bytes,free:p.unlimited?null:p.free_bytes,reserved:(s.users??[]).reduce((n,u)=>n+u.reserved_bytes,0),usable:p.usable_accounts,total:p.accounts,checkedAt:oldest((s.accounts??[]).filter(a=>a.enabled).map(a=>a.quota_checked_at))}
    } else {
      const {overview:s}=await myDrive.overview()
      storage.value={enabled:s.enabled,used:s.used_bytes,files:s.file_count,capacity:s.quota_bytes,free:s.available_bytes,reserved:s.reserved_bytes}
    }
    storageError.value=false
  } catch { storageError.value=true; storage.value=null }
}
async function loadCompute() {
  try { const {data}=await apiClient.get<ComputeSummary>(props.admin?'/admin/compute':'/user/compute'); compute.value=data; computeError.value=false }
  catch { computeError.value=true; compute.value=null }
}
async function load() { if (busy.value) return; busy.value=true; try { await Promise.allSettled([loadStorage(),loadCompute()]) } finally { busy.value=false } }
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{ void load(); timer=setInterval(()=>{ if (!document.hidden) void load() },30000) })
onUnmounted(()=>clearInterval(timer))
</script>
