<template>
  <section class="space-y-4" data-testid="resource-overview">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('compute.dashboardTitle') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(admin ? 'compute.dashboardAdminScope' : 'compute.dashboardUserScope') }}</p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="busy || apiLoading" @click="refreshAll">
        <Icon name="refresh" size="sm" class="mr-1.5" />{{ t('common.refresh') }}
      </button>
    </header>
    <div role="tablist" :aria-label="t('compute.dashboardTitle')" class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
      <button v-for="(tab, index) in tabs" :id="`product-tab-${tab.id}`" :key="tab.id" role="tab"
        :aria-selected="active === tab.id" :aria-controls="`product-panel-${tab.id}`" :tabindex="active === tab.id ? 0 : -1"
        class="flex items-center gap-2 border-b-2 px-5 py-3 text-sm font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
        :class="active === tab.id ? 'border-primary-500 text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
        @click="active = tab.id" @keydown="moveTab($event, index)">
        <Icon v-if="tab.id === 'api'" name="key" size="md" aria-hidden="true" />
        <StorageIcon v-else-if="tab.id === 'storage'" class="h-5 w-5" aria-hidden="true" />
        <ComputeIcon v-else class="h-5 w-5" aria-hidden="true" />
        {{ tab.title }}
      </button>
    </div>
    <div>
      <div v-show="active === 'api'" id="product-panel-api" role="tabpanel" aria-labelledby="product-tab-api" tabindex="0">
        <ProductSummaryCard data-testid="api-summary" title="API" :scope="t('compute.dashboardAPIScope')" :to="admin ? '/admin/usage' : '/usage'"
          :metrics="apiMetrics" :loading="apiLoading" :error="apiError ? t('compute.dashboardAPIError') : undefined">
          <template #icon><Icon name="key" size="md" class="text-blue-600 dark:text-blue-400" /></template>
          <template #content><slot v-if="active === 'api'" name="api" /></template>
        </ProductSummaryCard>
      </div>
      <div v-show="active === 'storage'" id="product-panel-storage" role="tabpanel" aria-labelledby="product-tab-storage" tabindex="0">
        <ProductSummaryCard data-testid="storage-summary" :title="t('compute.navStorage')" :scope="t('compute.dashboardStorageScope')" :to="admin ? '/admin/drive' : '/drive'"
          :metrics="storageStats" :loading="busy" :error="storageError ? t('compute.dashboardStorageError') : undefined">
          <template #icon><StorageIcon class="h-5 w-5 text-emerald-600 dark:text-emerald-400" /></template>
          <template v-if="storage">
            {{ t(storage.enabled ? 'compute.dashboardStorageReady' : 'compute.dashboardStorageDisabled') }}
            <span v-if="admin"> · {{ t('compute.dashboardSnapshot') }} {{ stamp(storage.checkedAt) }}</span>
          </template>
        </ProductSummaryCard>
      </div>
      <div v-show="active === 'compute'" id="product-panel-compute" role="tabpanel" aria-labelledby="product-tab-compute" tabindex="0">
        <ProductSummaryCard data-testid="compute-summary" :title="t('compute.navCompute')" :scope="t('compute.dashboardComputeScope')" :to="admin ? '/admin/compute' : '/compute'"
          :metrics="computeStats" :loading="busy" :error="computeError ? t('compute.dashboardComputeError') : undefined">
          <template #icon><ComputeIcon class="h-5 w-5 text-purple-600 dark:text-purple-400" /></template>
          <template v-if="compute">
            <p v-if="admin">
              {{ t('compute.dashboardCUCoverage', { known: compute.cu_accounts_known ?? 0, total: compute.accounts?.length ?? 0 }) }} · {{ t('compute.dashboardSnapshot') }} {{ stamp(cuCheckedAt) }}
              <span v-if="cuRefreshFailed" class="text-amber-600"> · {{ t('compute.cuStale') }}</span>
            </p>
            <p>{{ t(compute.compute_billing_enabled ? 'compute.billingEnabledHint' : 'compute.billingHint') }}</p>
          </template>
        </ProductSummaryCard>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import type { DashboardMetric } from './types'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { status as adminDriveStatus } from '@/api/admin/drive'
import { myDrive, formatBytes } from '@/api/drive'
import Icon from '@/components/icons/Icon.vue'
import ProductSummaryCard from './ProductSummaryCard.vue'
import StorageIcon from '@/components/icons/StorageIcon.vue'
import ComputeIcon from '@/components/icons/ComputeIcon.vue'

interface APIOverview {
  today_requests?: number; total_requests?: number; today_tokens?: number; total_tokens?: number
  today_actual_cost?: number; today_account_cost?: number; total_actual_cost?: number
  total_api_keys?: number; active_api_keys?: number; total_accounts?: number; normal_accounts?: number
  rpm?: number; tpm?: number
}
const props = defineProps<{ admin?: boolean; apiStats?: APIOverview | null; apiLoading?: boolean; apiError?: boolean }>()
const emit = defineEmits<{ refresh: [] }>()
const { t } = useI18n()
const active = ref<'api' | 'storage' | 'compute'>('api')
const tabs = computed(() => [
  { id: 'api' as const, title: 'API' },
  { id: 'storage' as const, title: t('compute.navStorage') },
  { id: 'compute' as const, title: t('compute.navCompute') }
])
async function moveTab(event: KeyboardEvent, index: number) {
  const count = tabs.value.length
  const target = event.key === 'ArrowRight' ? (index + 1) % count
    : event.key === 'ArrowLeft' ? (index + count - 1) % count
    : event.key === 'Home' ? 0 : event.key === 'End' ? count - 1 : -1
  if (target < 0) return
  event.preventDefault()
  active.value = tabs.value[target].id
  await nextTick()
  document.getElementById(`product-tab-${active.value}`)?.focus()
}
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
const apiMetrics = computed<DashboardMetric[]>(() => {
  const a = props.apiStats
  if (!a || props.apiError) return []
  const total = (value?: number) => `${t('common.total')}: ${number(value)}`
  return [
    { icon: 'chart', label: t('dashboard.todayRequests'), value: number(a.today_requests), detail: total(a.total_requests) },
    { icon: 'bolt', label: t('dashboard.todayTokens'), value: number(a.today_tokens), detail: total(a.total_tokens) },
    { icon: 'creditCard', label: t('compute.dashboardAPICharged'), value: money(a.today_actual_cost), detail: `${t('common.total')}: ${money(a.total_actual_cost)}` },
    ...(props.admin ? [{ icon: 'database', label: t('compute.dashboardAPICost'), value: money(a.today_account_cost) }] : []),
    { icon: 'key', label: t('dashboard.apiKeys'), value: number(a.active_api_keys)+' / '+number(a.total_api_keys), detail: t('compute.dashboardActiveTotal') },
    ...(props.admin ? [{ icon: 'server' as const, label: t('admin.dashboard.accounts'), value: number(a.normal_accounts)+' / '+number(a.total_accounts), detail: t('compute.dashboardActiveTotal') }] : []),
    { icon: 'clock', label: 'RPM', value: number(a.rpm) },
    { icon: 'bolt', label: 'TPM', value: number(a.tpm) }
  ]
})
function refreshAll() { emit('refresh'); void load() }
const cuCheckedAt = computed(() => oldest((compute.value?.accounts ?? []).filter(x => x.consumption?.currentBalance != null).map(x => x.consumption_checked_at)))
const cuRefreshFailed = computed(() => compute.value?.accounts?.some(x => !!x.consumption_error))
const storageStats = computed<DashboardMetric[]>(() => {
  const s=storage.value; if (!s) return []
  return [
    {icon:'folder',label:t(props.admin ? 'compute.dashboardStored' : 'compute.dashboardUsed'),value:formatBytes(s.used)},
    {icon:'document',label:t('compute.dashboardFiles'),value:number(s.files)},
    {icon:'database',tone:'stat-icon-success',label:t(props.admin ? 'compute.dashboardPoolFree' : 'compute.dashboardFree'),value:formatBytes(s.free)},
    {icon:'server',label:t(props.admin ? 'compute.dashboardCapacity' : 'compute.dashboardQuota'),value:s.capacity == null ? t('compute.dashboardUnknown') : formatBytes(s.capacity)},
    {icon:'upload',label:t('compute.dashboardStorageReserved'),value:formatBytes(s.reserved)},
    ...(props.admin ? [{icon:'server',label:t('compute.dashboardStorageAccounts'),value:`${s.usable} / ${s.total}`}] : [])
  ]
})
const computeStats = computed<DashboardMetric[]>(() => {
  const c=compute.value; if (!c) return []; const u=c.usage_summary
  return [
    {icon:'bolt',label:t(props.admin ? 'compute.googleCU' : 'compute.platformCredits'),value:number(props.admin ? c.compute_units : c.platform_credits,4)},
    {icon:'play',label:t('compute.dashboardRunning'),value:number(u.running_jobs)},
    {icon:'clock',label:t('compute.dashboardQueued'),value:number(u.queued_jobs)},
    {icon:'checkCircle',tone:'stat-icon-success',label:t('compute.dashboardSucceeded'),value:number(u.succeeded_jobs)+' / '+number(u.total_jobs)},
    {icon:'clock',label:t('compute.dashboardGPUHours'),value:number(u.gpu_runtime_seconds/3600,3)},
    {icon:'exclamationCircle',tone:'stat-icon-danger',label:t('compute.dashboardFailed'),value:number(u.failed_jobs)},
    {icon:'creditCard',label:t('compute.dashboardCharged'),value:money(u.charged_cost)},
    {icon:'lock',tone:'stat-icon-warning',label:t('compute.dashboardHeld'),value:money(u.reserved_cost)}
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
