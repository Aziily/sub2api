<template>
  <AppLayout>
    <div class="space-y-6 pb-8">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1
            class="flex items-center gap-2 text-xl font-bold text-gray-900 dark:text-white"
          >
            <ComputeIcon class="h-6 w-6 text-primary-600" />{{
              t(admin ? "compute.adminTitle" : "compute.title")
            }}
          </h1>
          <p class="mt-2 text-sm text-gray-500">
            {{ t("compute.description") }}
          </p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="busy" @click="load">
            {{ t("common.refresh") }}</button
          ><button
            v-if="admin"
            class="btn btn-primary"
            :disabled="busy || !status?.configured"
            @click="authorize"
          >
            {{ t("compute.connect") }}
          </button>
        </div>
      </header>
      <p
        v-if="error"
        role="alert"
        class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20"
      >
        {{ error }}
      </p>
      <section
        class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
        aria-label="Overview"
      >
        <div class="card p-5">
          <p class="text-xs text-gray-500">{{ t("compute.availability") }}</p>
          <p
            class="mt-2 text-lg font-semibold"
            :class="status?.ready ? 'text-emerald-600' : 'text-amber-600'"
          >
            {{
              status
                ? t(status.ready ? "compute.ready" : "compute.unavailable")
                : "—"
            }}
          </p>
          <p class="mt-1 text-xs text-gray-500">
            {{ t("compute.singleTask") }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-xs text-gray-500">
            {{ t("compute.platformCredits") }}
          </p>
          <p
            class="mt-2 text-2xl font-semibold tabular-nums"
            data-testid="compute-credits"
          >
            {{
              status?.platform_credits == null
                ? "—"
                : status.platform_credits.toLocaleString(undefined, {
                    maximumFractionDigits: 4,
                  })
            }}
          </p>
          <p class="mt-1 text-xs text-gray-500">
            {{ t("compute.billingHint") }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-xs text-gray-500">{{ t("compute.jobsTotal") }}</p>
          <p class="mt-2 text-2xl font-semibold tabular-nums">
            {{ status?.usage_summary?.total_jobs ?? "—" }}
          </p>
          <p class="mt-1 text-xs text-gray-500">
            {{ t("compute.jobsSucceeded") }}
            {{ status?.usage_summary?.succeeded_jobs ?? "—" }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-xs text-gray-500">{{ t("compute.gpuTime") }}</p>
          <p class="mt-2 text-2xl font-semibold tabular-nums">
            {{
              status
                ? fmtDur(status.usage_summary?.gpu_runtime_seconds || 0)
                : "—"
            }}
          </p>
          <p class="mt-1 text-xs text-gray-500">
            {{ t("compute.totalTime") }}
            {{ fmtDur(status?.usage_summary?.total_runtime_seconds || 0) }}
          </p>
        </div>
      </section>
      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 class="font-semibold">{{ t("compute.resources") }}</h2>
          <p class="mt-1 text-xs text-gray-500">{{ t("compute.limits") }}</p>
        </div>
        <div
          class="grid divide-y divide-gray-100 sm:grid-cols-2 xl:grid-cols-3 dark:divide-dark-700"
        >
          <button
            v-for="hw in hardwareChoices"
            :key="hw.id"
            type="button"
            class="flex items-center gap-4 p-5 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/50"
            :class="
              hardware === hw.id
                ? 'bg-primary-50/60 dark:bg-primary-900/10'
                : ''
            "
            @click="hardware = hw.id"
          >
            <ComputeIcon
              class="h-8 w-8 shrink-0"
              :class="hardware === hw.id ? 'text-primary-600' : 'text-gray-400'"
            />
            <div>
              <p class="text-sm font-semibold">{{ hw.label }}</p>
              <p class="mt-1 text-xs text-gray-500">
                {{
                  hw.accelerator === "NONE"
                    ? t("compute.cpuMemory")
                    : hw.vram_gb
                      ? `${hw.vram_gb} GB · ${t("compute.memory")}`
                      : t("compute.memoryUnknown")
                }}
              </p>
              <p v-if="hw.vram_gb" class="mt-0.5 text-xs text-gray-400">
                {{
                  t(
                    hw.memory_source === "observed"
                      ? "compute.memoryObserved"
                      : "compute.memoryNominal",
                  )
                }}
              </p>
            </div>
          </button>
        </div>
        <p v-if="!hardwareChoices.length" class="p-5 text-sm text-gray-500">
          {{ t("compute.noResources") }}
        </p>
        <p
          v-if="admin && status?.disabled_reason"
          class="px-5 pb-4 text-sm text-amber-600"
        >
          {{ status.disabled_reason }}
        </p>
      </section>
      <section class="card p-5">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 class="font-semibold">{{ t("compute.cliTitle") }}</h2>
            <p class="mt-2 max-w-3xl text-sm text-gray-500">
              {{ t("compute.cliDescription") }}
            </p>
          </div>
          <a
            class="btn btn-secondary btn-sm"
            href="/compute/v1/cli"
            download="compute.py"
            >{{ t("compute.cliDownload") }}</a
          >
        </div>
        <pre
          class="mt-4 overflow-x-auto rounded-lg bg-gray-950 p-4 text-xs leading-6 text-gray-100"
          >{{ cliExample }}</pre
        >
        <div class="mt-3 space-y-2 text-xs text-gray-500">
          <p>{{ t("compute.cliIgnore") }}</p>
          <p>{{ t("compute.cliTransfer") }}</p>
          <RouterLink
            class="inline-block text-primary-600 hover:underline"
            to="/keys"
            >{{ t("compute.cliKey") }}</RouterLink
          >
        </div>
      </section>
      <section v-if="admin" class="card overflow-x-auto p-5">
        <h2 class="mb-4 font-semibold">{{ t("compute.accounts") }}</h2>
        <p v-if="!status?.accounts?.length" class="text-sm text-gray-500">
          {{ t("compute.noAccounts") }}
        </p>
        <table v-else class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900/40">
            <tr>
              <th class="py-2">{{ t("compute.account") }}</th>
              <th>{{ t("compute.plan") }}</th>
              <th>{{ t("compute.gpuEligibility") }}</th>
              <th>{{ t("compute.accountUsage") }}</th>
              <th>{{ t("compute.status") }}</th>
              <th>{{ t("compute.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="a in status.accounts"
              :key="a.id"
              class="border-t border-gray-100 dark:border-dark-700"
            >
              <td class="py-4">
                {{ a.email }}
                <div class="text-xs text-gray-500">
                  {{ stamp(a.checked_at) }}
                </div>
              </td>
              <td>{{ a.subscription?.tier || "—" }}</td>
              <td class="py-3">
                <div class="flex max-w-md flex-wrap gap-1.5">
                  <span
                    v-for="hw in a.hardware_specs || []"
                    :key="hw.id"
                    class="rounded px-2 py-0.5 text-xs font-medium"
                    :class="
                      hw.eligible
                        ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                        : 'bg-gray-100 text-gray-400 line-through dark:bg-dark-700'
                    "
                    >{{ hw.label }}</span
                  >
                </div>
              </td>
              <td class="text-xs">
                <div>
                  A100: {{ fmtDur(a.a100_runtime_seconds || 0) }} · GPU:
                  {{ fmtDur(a.gpu_runtime_seconds || 0) }}
                </div>
                <div class="text-gray-500">
                  {{ t("compute.totalTime") }}:
                  {{ fmtDur(a.total_runtime_seconds || 0) }} ({{
                    a.job_count || 0
                  }})
                </div>
              </td>
              <td>
                <p>
                  {{ t(a.enabled ? "compute.enabled" : "compute.disabled") }}
                </p>
                <p v-if="a.last_error" class="max-w-sm text-xs text-red-600">
                  {{ a.last_error }}
                </p>
              </td>
              <td class="space-x-2">
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="busy"
                  @click="refreshAccount(a.id)"
                >
                  {{ t("common.refresh") }}</button
                ><button
                  class="btn btn-secondary btn-sm"
                  :disabled="busy"
                  @click="toggle(a)"
                >
                  {{ t(a.enabled ? "compute.disable" : "compute.enable") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <p class="mt-4 text-xs text-gray-500">
          {{ t("compute.balance") }}: {{ t("compute.notProvided") }}
        </p>
        <p class="mt-2 break-all text-xs text-gray-500">
          {{ t("compute.callback") }}: {{ status?.redirect_uri }}
        </p>
      </section>
      <details class="card p-5">
        <summary class="cursor-pointer text-sm font-semibold">
          {{ t("compute.webSubmit") }}
        </summary>
        <div
          class="mb-4 mt-4 flex flex-wrap items-center justify-between gap-2"
        >
          <h2 class="font-semibold">{{ t("compute.newJob") }}</h2>
          <div class="flex gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="usePreset('A100')"
            >
              {{ t("compute.presetA100") }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="usePreset('CPU')"
            >
              {{ t("compute.presetCPU") }}
            </button>
          </div>
        </div>
        <form class="space-y-4" @submit.prevent="submit">
          <div class="grid gap-4 sm:grid-cols-[1fr_240px_160px]">
            <label class="text-sm"
              >{{ t("compute.jobTitle")
              }}<input
                v-model="title"
                class="input mt-1 w-full"
                maxlength="120"
                required
            /></label>
            <label class="text-sm"
              >{{ t("compute.selectHardware") }}
              <select v-model="hardware" class="input mt-1 w-full">
                <option
                  v-for="hw in hardwareChoices"
                  :key="hw.id"
                  :value="hw.id"
                  :disabled="!hw.eligible"
                >
                  {{ hw.label }} ({{
                    t(hw.eligible ? "compute.eligible" : "compute.ineligible")
                  }})
                </option>
              </select>
            </label>
            <label class="text-sm"
              >{{ t("compute.timeout")
              }}<input
                v-model.number="timeout"
                type="number"
                class="input mt-1 w-full"
                min="30"
                max="3600"
                required
            /></label>
          </div>
          <label class="block text-sm"
            >Python<textarea
              v-model="code"
              class="input mt-1 min-h-48 w-full font-mono text-sm"
              spellcheck="false"
              maxlength="65536"
              required
            />
          </label>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="max-w-2xl text-xs text-gray-500">
              {{ t("compute.outputHelp") }}
            </p>
            <button class="btn btn-primary" :disabled="busy || !status?.ready">
              {{ t("compute.submit") }}
            </button>
          </div>
        </form>
      </details>
      <section class="card overflow-x-auto p-5">
        <h2 class="mb-4 font-semibold">
          {{ t(admin ? "compute.allJobs" : "compute.myJobs") }}
        </h2>
        <p v-if="!jobs.length" class="py-6 text-sm text-gray-500">
          {{ t("compute.empty") }}
        </p>
        <table v-else class="w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900/40">
            <tr>
              <th class="py-2">{{ t("compute.jobTitle") }}</th>
              <th>{{ t("compute.selectHardware") }}</th>
              <th>{{ t("compute.duration") }}</th>
              <th v-if="admin">{{ t("compute.owner") }}</th>
              <th>{{ t("compute.status") }}</th>
              <th>{{ t("compute.created") }}</th>
              <th>{{ t("compute.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="j in jobs"
              :key="j.id"
              class="border-t border-gray-100 dark:border-dark-700"
            >
              <td class="py-4">
                {{ j.title }}
                <div class="text-xs text-gray-500">{{ j.id.slice(0, 8) }}</div>
              </td>
              <td>
                <span
                  class="rounded bg-gray-100 px-2 py-0.5 text-xs font-medium dark:bg-dark-700"
                  >{{ j.hardware || "CPU" }}</span
                >
              </td>
              <td>
                {{ j.allocated_at ? fmtDur(j.duration_seconds || 0) : "—" }}
              </td>
              <td v-if="admin">#{{ j.user_id }}</td>
              <td>{{ stateLabel(j.status) }}</td>
              <td>{{ stamp(j.created_at) }}</td>
              <td>
                <button class="btn btn-secondary btn-sm" @click="show(j.id)">
                  {{ t("compute.details") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <p class="mt-4 text-xs text-gray-500">
          {{ t("compute.recordsLimit") }} {{ t("compute.runtimeHint") }}
        </p>
      </section>
    </div>
    <Teleport to="body"
      ><div
        v-if="selected"
        class="fixed inset-0 z-50 flex justify-end"
        @keydown.esc="selected = null"
      >
        <button
          class="absolute inset-0 bg-black/30"
          :aria-label="t('common.close')"
          @click="selected = null"
        />
        <section
          role="dialog"
          aria-modal="true"
          :aria-label="selected.title"
          class="relative h-full w-full max-w-2xl overflow-y-auto bg-white p-6 shadow-xl dark:bg-dark-800"
        >
          <div class="flex justify-between gap-4">
            <h2 class="text-lg font-bold">{{ selected.title }}</h2>
            <button class="btn btn-secondary btn-sm" @click="selected = null">
              {{ t("common.close") }}
            </button>
          </div>
          <p class="mt-3 text-sm">
            {{ selected.hardware || "CPU" }} ·
            {{ fmtDur(selected.duration_seconds || 0) }} ·
            {{ stateLabel(selected.status) }} · {{ t("compute.runtime") }}:
            {{
              selected.runtime_released
                ? t("compute.released")
                : selected.runtime_name
                  ? t("compute.pendingRelease")
                  : "—"
            }}
          </p>
          <p
            v-if="selected.error"
            role="alert"
            class="mt-4 text-sm text-red-600"
          >
            {{ selected.error }}
          </p>
          <p v-if="selected.output_path" class="mt-4 break-all text-sm">
            {{ t("compute.output") }}:
            <RouterLink class="text-primary-600 underline" to="/drive">{{
              selected.output_path
            }}</RouterLink>
          </p>
          <button
            v-if="active(selected.status)"
            class="btn btn-secondary mt-4"
            @click="cancel(selected.id)"
          >
            {{ t("compute.cancel") }}
          </button>
          <h3 class="mt-6 font-semibold">{{ t("compute.logs") }}</h3>
          <pre
            class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-950 p-4 text-xs text-gray-100"
            >{{ selected.logs || t("compute.noLogs") }}</pre
          >
          <h3 class="mt-6 font-semibold">Python</h3>
          <pre
            class="mt-2 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-100 p-4 text-xs dark:bg-dark-900"
            >{{ selected.code }}</pre
          >
        </section>
      </div></Teleport
    >
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import AppLayout from "@/components/layout/AppLayout.vue";
import api from "@/api/client";
import ComputeIcon from "@/components/icons/ComputeIcon.vue";
interface HardwareOption {
  id: string;
  label: string;
  variant: string;
  accelerator: string;
  shape: string;
  eligible: boolean;
  high_mem?: boolean;
  vram_gb?: number;
  memory_source?: string;
}
interface UsageSummary {
  total_jobs: number;
  succeeded_jobs: number;
  total_runtime_seconds: number;
  gpu_runtime_seconds: number;
  a100_runtime_seconds: number;
}
interface Account {
  id: number;
  email: string;
  enabled: boolean;
  subscription: { tier?: string };
  hardware_specs?: HardwareOption[];
  job_count?: number;
  total_runtime_seconds?: number;
  gpu_runtime_seconds?: number;
  a100_runtime_seconds?: number;
  last_error: string;
  checked_at: string | null;
}
interface Status {
  platform_credits?: number;
  configured: boolean;
  ready: boolean;
  disabled_reason: string;
  hardware?: string;
  hardware_options?: HardwareOption[];
  usage_summary?: UsageSummary;
  redirect_uri?: string;
  accounts?: Account[];
}
interface Job {
  allocated_at?: string | null;
  id: string;
  title: string;
  code: string;
  hardware: string;
  status: string;
  duration_seconds: number;
  user_id: number;
  runtime_name: string;
  runtime_released: boolean;
  logs: string;
  error: string;
  output_path: string;
  created_at: string;
}
const { t } = useI18n(),
  route = useRoute();
const admin = computed(() => route.path.startsWith("/admin/"));
const base = computed(() => (admin.value ? "/admin/compute" : "/user/compute"));
const status = ref<Status | null>(null),
  jobs = ref<Job[]>([]),
  selected = ref<Job | null>(null),
  busy = ref(false),
  error = ref("");
const title = ref("CPU smoke test"),
  hardware = ref("CPU"),
  timeout = ref(180),
  code = ref(
    'import json, platform, subprocess\nsmi = subprocess.run(["nvidia-smi", "--query-gpu=name,memory.total,driver_version", "--format=csv,noheader"], capture_output=True, text=True)\nprint(json.dumps({"message": "COLAB_GPU_OK", "gpu": smi.stdout.strip(), "machine": platform.machine()}))',
  );
const hardwareChoices = computed<HardwareOption[]>(
  () => status.value?.hardware_options?.filter((hw) => hw.eligible) || [],
);
const cliExample = computed(
  () => `export SUB2API_BASE_URL="${window.location.origin}"
# SUB2API_API_KEY: ${t("compute.cliKey")}
python3 compute.py run --hardware ${hardware.value} --output outputs --wait -- python main.py`,
);
function fmtDur(sec: number) {
  if (!sec || sec <= 0) return "0s";
  const m = Math.floor(sec / 60),
    s = sec % 60;
  if (m < 60) return m > 0 ? `${m}m ${s}s` : `${s}s`;
  const h = Math.floor(m / 60),
    rm = m % 60;
  return `${h}h ${rm}m`;
}
function usePreset(kind: "A100" | "CPU") {
  if (kind === "A100") {
    title.value = "A100 GPU verification";
    hardware.value = "A100";
    code.value =
      'import json, platform, subprocess\nsmi = subprocess.run(["nvidia-smi", "--query-gpu=name,memory.total,driver_version", "--format=csv,noheader"], capture_output=True, text=True)\nprint(json.dumps({"message": "COLAB_GPU_OK", "gpu": smi.stdout.strip(), "machine": platform.machine()}))';
  } else {
    title.value = "CPU smoke test";
    hardware.value = "CPU";
    code.value =
      'import json, platform\nprint(json.dumps({"message": "COLAB_CPU_OK", "sum": sum(i*i for i in range(10000)), "machine": platform.machine()}))';
  }
}
const stamp = (v: string | null) => (v ? new Date(v).toLocaleString() : "—");
const active = (v: string) =>
  ["queued", "provisioning", "running", "writing"].includes(v);
const stateLabel = (v: string) => t("compute.states." + v);
function errorText(e: unknown) {
  return (e as { message?: string }).message || t("compute.failed");
}
let loading = false;
async function load() {
  if (loading) return;
  loading = true;
  try {
    const [s, j] = await Promise.all([
      api.get<Status>(base.value),
      api.get<Job[]>(base.value + "/jobs"),
    ]);
    status.value = s.data;
    jobs.value = j.data;
    error.value = "";
    if (!hardwareChoices.value.some((hw) => hw.id === hardware.value))
      hardware.value = hardwareChoices.value[0]?.id || "";
    if (selected.value) await show(selected.value.id);
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading = false;
  }
}
async function action(fn: () => Promise<void>) {
  busy.value = true;
  error.value = "";
  try {
    await fn();
    await load();
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
async function authorize() {
  await action(async () => {
    const r = await api.post<{ url: string }>(
      base.value + "/accounts/authorize",
    );
    window.location.assign(r.data.url);
  });
}
async function refreshAccount(id: number) {
  await action(async () => {
    await api.post(base.value + "/accounts/" + id + "/refresh");
  });
}
async function toggle(a: Account) {
  await action(async () => {
    await api.put(base.value + "/accounts/" + a.id, { enabled: !a.enabled });
  });
}
let submissionID: string | null = null;
watch([title, code, hardware, timeout], () => {
  submissionID = null;
});
async function submit() {
  await action(async () => {
    submissionID ||= crypto.randomUUID();
    const r = await api.post<Job>(base.value + "/jobs", {
      id: submissionID,
      title: title.value,
      code: code.value,
      hardware: hardware.value,
      timeout_seconds: timeout.value,
    });
    submissionID = null;
    selected.value = r.data;
  });
}
async function show(id: string) {
  try {
    selected.value = (await api.get<Job>(base.value + "/jobs/" + id)).data;
  } catch (e) {
    error.value = errorText(e);
  }
}
async function cancel(id: string) {
  await action(async () => {
    await api.post(base.value + "/jobs/" + id + "/cancel");
  });
}
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  usePreset("CPU");
  void load();
  timer = setInterval(() => {
    if (!busy.value) void load();
  }, 5000);
});
onUnmounted(() => {
  if (timer) clearInterval(timer);
});
</script>
