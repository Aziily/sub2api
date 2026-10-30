<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition-opacity duration-200"
      leave-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      leave-to-class="opacity-0"
    >
      <div v-if="show" class="fixed inset-0 z-40 bg-gray-900/30 backdrop-blur-[1px]" @click="close" />
    </Transition>
    <Transition
      enter-active-class="transition-transform duration-300 ease-out"
      leave-active-class="transition-transform duration-200 ease-in"
      enter-from-class="translate-x-full"
      leave-to-class="translate-x-full"
    >
      <aside
        v-if="show"
        class="fixed inset-y-0 right-0 z-50 flex w-full max-w-2xl flex-col bg-white shadow-2xl dark:bg-dark-800"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        data-testid="drive-drawer"
        @keydown.esc="close"
      >
        <header class="flex items-center justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div class="min-w-0">
            <h2 class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ title }}</h2>
            <p v-if="subtitle" class="truncate text-xs text-gray-500 dark:text-gray-400">{{ subtitle }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-sm" :aria-label="t('common.close')" @click="close">
            <Icon name="x" size="sm" />
          </button>
        </header>

        <div class="flex flex-wrap items-center gap-2 border-b border-gray-100 px-5 py-3 dark:border-dark-700">
          <nav class="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-sm">
            <button type="button" class="font-medium text-primary-600 hover:underline dark:text-primary-400" @click="go('/')">
              {{ t('drive.root') }}
            </button>
            <template v-for="crumb in crumbs" :key="crumb.path">
              <span class="text-gray-300">/</span>
              <button type="button" class="max-w-[10rem] truncate text-primary-600 hover:underline dark:text-primary-400" @click="go(crumb.path)">
                {{ crumb.name }}
              </button>
            </template>
          </nav>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="startFolder">
            <Icon name="plus" size="sm" class="mr-1" />{{ t('drive.newFolder') }}
          </button>
          <label class="btn btn-primary btn-sm cursor-pointer" :class="busy ? 'pointer-events-none opacity-60' : ''">
            <Icon name="upload" size="sm" class="mr-1" />{{ t('drive.upload') }}
            <input type="file" class="hidden" multiple data-testid="drive-upload-input" @change="uploadFiles" />
          </label>
        </div>

        <div v-if="naming" class="flex items-center gap-2 border-b border-gray-100 bg-gray-50 px-5 py-3 dark:border-dark-700 dark:bg-dark-900/40">
          <input
            ref="nameInput"
            v-model="nameValue"
            class="input h-9 flex-1"
            :placeholder="naming.kind === 'folder' ? t('drive.folderName') : t('drive.newName')"
            @keyup.enter="submitName"
            @keyup.esc.stop="naming = null"
          />
          <button type="button" class="btn btn-primary btn-sm" :disabled="!nameValue.trim() || busy" @click="submitName">
            {{ t('common.confirm') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="naming = null">{{ t('common.cancel') }}</button>
        </div>

        <p v-if="uploadStatus" class="border-b border-gray-100 px-5 py-2 text-xs text-gray-500 dark:border-dark-700" data-testid="drive-upload-status">
          {{ uploadStatus }}
        </p>

        <div class="min-h-0 flex-1 overflow-y-auto">
          <ul class="divide-y divide-gray-50 dark:divide-dark-700/60">
            <li v-if="currentPath !== '/'">
              <button type="button" class="flex w-full items-center gap-3 px-5 py-2.5 text-left text-sm text-gray-500 hover:bg-gray-50 dark:hover:bg-dark-700/40" @click="go(parentPath)">
                <Icon name="arrowLeft" size="sm" />{{ t('drive.up') }}
              </button>
            </li>
            <li v-for="item in entries" :key="item.path" class="group flex items-center gap-3 px-5 py-2.5 hover:bg-gray-50 dark:hover:bg-dark-700/40">
              <span
                class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
                :class="item.is_dir ? 'bg-amber-50 text-amber-500 dark:bg-amber-900/20' : 'bg-gray-100 text-gray-400 dark:bg-dark-700'"
              >
                <Icon :name="item.is_dir ? 'folder' : 'document'" size="sm" />
              </span>
              <div class="min-w-0 flex-1">
                <button
                  v-if="item.is_dir"
                  type="button"
                  class="block max-w-full truncate text-left text-sm font-medium text-gray-900 hover:text-primary-600 dark:text-white"
                  @click="go(item.path)"
                >
                  {{ item.name }}
                </button>
                <span v-else class="block truncate text-sm text-gray-900 dark:text-white">{{ item.name }}</span>
                <span class="text-xs text-gray-400">
                  {{ item.is_dir ? t('drive.folder') : formatBytes(item.size) }} · {{ formatTime(item.modified) }}
                </span>
              </div>
              <div class="flex shrink-0 items-center gap-1 opacity-80 group-hover:opacity-100">
                <button v-if="!item.is_dir" type="button" class="btn btn-ghost btn-sm" :title="t('drive.download')" :disabled="busy" @click="downloadFile(item)">
                  <Icon name="download" size="sm" />
                </button>
                <button type="button" class="btn btn-ghost btn-sm" :title="t('drive.rename')" :disabled="busy" @click="startRename(item)">
                  <Icon name="edit" size="sm" />
                </button>
                <button type="button" class="btn btn-ghost btn-sm text-red-500" :title="t('common.delete')" :disabled="busy" @click="pendingDelete = item">
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </li>
          </ul>
          <div v-if="listing" class="py-10 text-center text-sm text-gray-400">{{ t('common.loading') }}</div>
          <div v-else-if="!entries.length" class="py-12 text-center text-sm text-gray-400">{{ t('drive.empty') }}</div>
        </div>

        <footer class="border-t border-gray-100 px-5 py-3 text-xs text-gray-400 dark:border-dark-700">
          {{ t('drive.drawerFooter', { files: entries.filter((e) => !e.is_dir).length, folders: entries.filter((e) => e.is_dir).length }) }}
        </footer>
      </aside>
    </Transition>

    <ConfirmDialog
      :show="!!pendingDelete"
      :title="t('common.delete')"
      :message="t(pendingDelete?.is_dir ? 'drive.confirmDeleteFolder' : 'drive.confirmDelete', { name: pendingDelete?.name ?? '' })"
      danger
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatBytes, type DriveClient, type DriveEntry } from '@/api/drive'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
  client: DriveClient | null
  title: string
  subtitle?: string
}>()
const emit = defineEmits<{ close: []; changed: [] }>()

const { t } = useI18n()
const appStore = useAppStore()

const currentPath = ref('/')
const entries = ref<DriveEntry[]>([])
const listing = ref(false)
const busy = ref(false)
const uploadStatus = ref('')
const pendingDelete = ref<DriveEntry | null>(null)
const naming = ref<{ kind: 'folder' } | { kind: 'rename'; item: DriveEntry } | null>(null)
const nameValue = ref('')
const nameInput = ref<HTMLInputElement | null>(null)

const crumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  return parts.map((name, i) => ({ name, path: '/' + parts.slice(0, i + 1).join('/') }))
})
const parentPath = computed(() => '/' + currentPath.value.split('/').filter(Boolean).slice(0, -1).join('/'))

function joinPath(dir: string, name: string) {
  return dir === '/' ? `/${name}` : `${dir}/${name}`
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? '-' : date.toLocaleString()
}

function showError(err: unknown) {
  appStore.showError(extractApiErrorMessage(err, t('drive.requestFailed')))
}

function close() {
  if (!busy.value) emit('close')
}

async function load() {
  if (!props.client) return
  listing.value = true
  try {
    entries.value = await props.client.list(currentPath.value)
  } catch (err) {
    entries.value = []
    showError(err)
  } finally {
    listing.value = false
  }
}

function go(path: string) {
  currentPath.value = path || '/'
  naming.value = null
  load()
}

watch(
  () => [props.show, props.client] as const,
  ([show]) => {
    if (show) go('/')
  },
  { immediate: true }
)

async function run(action: () => Promise<unknown>) {
  busy.value = true
  try {
    await action()
    emit('changed')
  } catch (err) {
    showError(err)
  } finally {
    busy.value = false
    await load()
  }
}

async function openNaming(state: NonNullable<typeof naming.value>, initial: string) {
  naming.value = state
  nameValue.value = initial
  await nextTick()
  nameInput.value?.focus()
  nameInput.value?.select()
}

function startFolder() {
  openNaming({ kind: 'folder' }, '')
}

function startRename(item: DriveEntry) {
  openNaming({ kind: 'rename', item }, item.name)
}

function submitName() {
  const state = naming.value
  const name = nameValue.value.trim()
  const client = props.client
  if (!state || !name || !client) return
  naming.value = null
  if (state.kind === 'folder') {
    run(() => client.mkdir(joinPath(currentPath.value, name)))
  } else if (name !== state.item.name) {
    run(() => client.move(state.item.path, joinPath(currentPath.value, name)))
  }
}

function uploadFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  const client = props.client
  if (!client || !files.length) return
  const dir = currentPath.value
  run(async () => {
    try {
      for (const file of files) {
        uploadStatus.value = t('drive.uploading', { name: file.name, percent: 0 })
        await client.upload(dir, file, (fraction) => {
          uploadStatus.value = t('drive.uploading', { name: file.name, percent: Math.floor(fraction * 100) })
        })
      }
      appStore.showSuccess(t('drive.uploaded', { count: files.length }))
    } finally {
      uploadStatus.value = ''
    }
  })
}

async function downloadFile(item: DriveEntry) {
  if (!props.client) return
  busy.value = true
  try {
    const blob = await props.client.download(item.path)
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

function confirmDelete() {
  const item = pendingDelete.value
  const client = props.client
  pendingDelete.value = null
  if (item && client) run(() => client.remove(item.path))
}
</script>
