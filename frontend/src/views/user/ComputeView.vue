<template>
  <AppLayout>
    <div class="space-y-6 pb-8">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div><h1 class="text-xl font-bold text-gray-900 dark:text-white">{{ t(admin ? 'compute.adminTitle' : 'compute.title') }}</h1><p class="mt-2 text-sm text-gray-500">{{ t('compute.description') }}</p></div>
        <div class="flex gap-2"><button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('common.refresh') }}</button><button v-if="admin" class="btn btn-primary" :disabled="busy || !status?.configured" @click="authorize">{{ t('compute.connect') }}</button></div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20">{{ error }}</p>
      <section class="card p-5">
        <div class="flex flex-wrap gap-x-12 gap-y-4">
          <div><p class="text-xs text-gray-500">{{ t('compute.availability') }}</p><p class="mt-1 font-semibold" :class="status?.ready ? 'text-emerald-600' : 'text-amber-600'">{{ t(status?.ready ? 'compute.ready' : 'compute.unavailable') }}</p></div>
          <div><p class="text-xs text-gray-500">{{ t('compute.hardware') }}</p><p class="mt-1 font-semibold">CPU · {{ t('compute.singleTask') }}</p></div>
          <div><p class="text-xs text-gray-500">{{ t('compute.balance') }}</p><p class="mt-1 font-semibold">{{ t('compute.notProvided') }}</p></div>
        </div>
        <p v-if="admin && status?.disabled_reason" class="mt-4 text-sm text-amber-600">{{ status.disabled_reason }}</p>
        <p class="mt-4 text-xs text-gray-500">{{ t('compute.limits') }}</p>
      </section>
      <section v-if="admin" class="card overflow-x-auto p-5">
        <h2 class="mb-4 font-semibold">{{ t('compute.accounts') }}</h2>
        <p v-if="!status?.accounts?.length" class="text-sm text-gray-500">{{ t('compute.noAccounts') }}</p>
        <table v-else class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-2">{{ t('compute.account') }}</th><th>{{ t('compute.plan') }}</th><th>{{ t('compute.status') }}</th><th>{{ t('compute.actions') }}</th></tr></thead><tbody><tr v-for="a in status.accounts" :key="a.id" class="border-t border-gray-100 dark:border-dark-700"><td class="py-4">{{ a.email }}<div class="text-xs text-gray-500">{{ stamp(a.checked_at) }}</div></td><td>{{ a.subscription?.tier || '—' }}</td><td><p>{{ t(a.enabled ? 'compute.enabled' : 'compute.disabled') }}</p><p v-if="a.last_error" class="max-w-sm text-xs text-red-600">{{ a.last_error }}</p></td><td class="space-x-2"><button class="btn btn-secondary btn-sm" :disabled="busy" @click="refreshAccount(a.id)">{{ t('common.refresh') }}</button><button class="btn btn-secondary btn-sm" :disabled="busy" @click="toggle(a)">{{ t(a.enabled ? 'compute.disable' : 'compute.enable') }}</button></td></tr></tbody></table>
        <p class="mt-4 break-all text-xs text-gray-500">{{ t('compute.callback') }}: {{ status?.redirect_uri }}</p>
      </section>
      <section v-else class="card p-5">
        <h2 class="mb-4 font-semibold">{{ t('compute.newJob') }}</h2>
        <form class="space-y-4" @submit.prevent="submit">
          <div class="grid gap-4 sm:grid-cols-[1fr_180px]"><label class="text-sm">{{ t('compute.jobTitle') }}<input v-model="title" class="input mt-1 w-full" maxlength="120" required /></label><label class="text-sm">{{ t('compute.timeout') }}<input v-model.number="timeout" type="number" class="input mt-1 w-full" min="30" max="300" required /></label></div>
          <label class="block text-sm">Python<textarea v-model="code" class="input mt-1 min-h-48 w-full font-mono text-sm" spellcheck="false" maxlength="65536" required /></label>
          <div class="flex flex-wrap items-center justify-between gap-3"><p class="max-w-2xl text-xs text-gray-500">{{ t('compute.outputHelp') }}</p><button class="btn btn-primary" :disabled="busy || !status?.ready">{{ t('compute.submit') }}</button></div>
        </form>
      </section>
      <section class="card overflow-x-auto p-5">
        <h2 class="mb-4 font-semibold">{{ t(admin ? 'compute.allJobs' : 'compute.myJobs') }}</h2>
        <p v-if="!jobs.length" class="py-6 text-sm text-gray-500">{{ t('compute.empty') }}</p>
        <table v-else class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-2">{{ t('compute.jobTitle') }}</th><th v-if="admin">{{ t('compute.owner') }}</th><th>{{ t('compute.status') }}</th><th>{{ t('compute.created') }}</th><th>{{ t('compute.actions') }}</th></tr></thead><tbody><tr v-for="j in jobs" :key="j.id" class="border-t border-gray-100 dark:border-dark-700"><td class="py-4">{{ j.title }}<div class="text-xs text-gray-500">{{ j.id.slice(0,8) }}</div></td><td v-if="admin">#{{ j.user_id }}</td><td>{{ stateLabel(j.status) }}</td><td>{{ stamp(j.created_at) }}</td><td><button class="btn btn-secondary btn-sm" @click="show(j.id)">{{ t('compute.details') }}</button></td></tr></tbody></table>
        <p class="mt-4 text-xs text-gray-500">{{ t('compute.recordsLimit') }}</p>
      </section>
    </div>
    <Teleport to="body"><div v-if="selected" class="fixed inset-0 z-50 flex justify-end" @keydown.esc="selected=null"><button class="absolute inset-0 bg-black/30" :aria-label="t('common.close')" @click="selected=null" /><section role="dialog" aria-modal="true" :aria-label="selected.title" class="relative h-full w-full max-w-2xl overflow-y-auto bg-white p-6 shadow-xl dark:bg-dark-800"><div class="flex justify-between gap-4"><h2 class="text-lg font-bold">{{ selected.title }}</h2><button class="btn btn-secondary btn-sm" @click="selected=null">{{ t('common.close') }}</button></div><p class="mt-3 text-sm">{{ stateLabel(selected.status) }} · {{ t('compute.runtime') }}: {{ selected.runtime_released ? t('compute.released') : selected.runtime_name ? t('compute.pendingRelease') : '—' }}</p><p v-if="selected.error" role="alert" class="mt-4 text-sm text-red-600">{{ selected.error }}</p><p v-if="selected.output_path" class="mt-4 break-all text-sm">{{ t('compute.output') }}: <RouterLink class="text-primary-600 underline" to="/drive">{{ selected.output_path }}</RouterLink></p><button v-if="active(selected.status)" class="btn btn-secondary mt-4" @click="cancel(selected.id)">{{ t('compute.cancel') }}</button><h3 class="mt-6 font-semibold">{{ t('compute.logs') }}</h3><pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-950 p-4 text-xs text-gray-100">{{ selected.logs || t('compute.noLogs') }}</pre><h3 class="mt-6 font-semibold">Python</h3><pre class="mt-2 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-100 p-4 text-xs dark:bg-dark-900">{{ selected.code }}</pre></section></div></Teleport>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import api from '@/api/client'
interface Account { id: number; email: string; enabled: boolean; subscription: {tier?:string}; last_error: string; checked_at: string | null }
interface Status { configured:boolean; ready:boolean; disabled_reason:string; redirect_uri?:string; accounts?:Account[] }
interface Job { id:string; title:string; code:string; status:string; user_id:number; runtime_name:string; runtime_released:boolean; logs:string; error:string; output_path:string; created_at:string }
const { t }=useI18n(), route=useRoute()
const admin=computed(()=>route.path.startsWith('/admin/'))
const base=computed(()=>admin.value?'/admin/compute':'/user/compute')
const status=ref<Status|null>(null), jobs=ref<Job[]>([]), selected=ref<Job|null>(null), busy=ref(false), error=ref('')
const title=ref('CPU smoke test'), timeout=ref(180), code=ref('import json, platform\nprint(json.dumps({"message": "COLAB_CPU_OK", "sum": sum(i*i for i in range(10000)), "machine": platform.machine()}))')
const stamp=(v:string|null)=>v?new Date(v).toLocaleString():'—'
const active=(v:string)=>['queued','provisioning','running','writing'].includes(v)
const stateLabel=(v:string)=>t('compute.states.'+v)
function errorText(e:unknown){return (e as {message?:string}).message || t('compute.failed')}
async function load(){try{const [s,j]=await Promise.all([api.get<Status>(base.value),api.get<Job[]>(base.value+'/jobs')]);status.value=s.data;jobs.value=j.data;if(selected.value)await show(selected.value.id)}catch(e){error.value=errorText(e)}}
async function action(fn:()=>Promise<void>){busy.value=true;error.value='';try{await fn();await load()}catch(e){error.value=errorText(e)}finally{busy.value=false}}
async function authorize(){await action(async()=>{const r=await api.post<{url:string}>(base.value+'/accounts/authorize');window.location.assign(r.data.url)})}
async function refreshAccount(id:number){await action(async()=>{await api.post(base.value+'/accounts/'+id+'/refresh')})}
async function toggle(a:Account){await action(async()=>{await api.put(base.value+'/accounts/'+a.id,{enabled:!a.enabled})})}
let submissionID:string|null=null
watch([title,code,timeout],()=>{submissionID=null})
async function submit(){await action(async()=>{submissionID ||= crypto.randomUUID();const r=await api.post<Job>(base.value+'/jobs',{id:submissionID,title:title.value,code:code.value,timeout_seconds:timeout.value});submissionID=null;selected.value=r.data})}
async function show(id:string){try{selected.value=(await api.get<Job>(base.value+'/jobs/'+id)).data}catch(e){error.value=errorText(e)}}
async function cancel(id:string){await action(async()=>{await api.post(base.value+'/jobs/'+id+'/cancel')})}
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void load();timer=setInterval(()=>{if(!busy.value)void load()},5000)})
onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>
