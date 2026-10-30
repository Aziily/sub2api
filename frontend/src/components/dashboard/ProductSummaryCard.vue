<template>
  <article class="card flex min-w-0 flex-col p-5" :aria-busy="loading">
    <header class="mb-5 flex items-start justify-between gap-3">
      <div>
        <h3 class="flex items-center gap-2.5 text-base font-semibold text-gray-900 dark:text-white">
          <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700"><slot name="icon" /></span>
          {{ title }}
        </h3>
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ scope }}</p>
      </div>
      <RouterLink :to="to" class="shrink-0 rounded text-sm text-primary-600 hover:underline focus-visible:outline focus-visible:outline-primary-500">{{ t('compute.dashboardOpen') }}</RouterLink>
    </header>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <p v-else-if="!metrics.length" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <dl v-else class="grid grid-cols-2 gap-x-5 gap-y-5">
      <div v-for="item in metrics" :key="item.label" class="min-w-0">
        <dt class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ item.label }}</dt>
        <dd class="mt-1 break-words text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</dd>
        <dd v-if="item.detail" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ item.detail }}</dd>
      </div>
    </dl>
    <footer v-if="$slots.default" class="mt-auto pt-5 text-xs leading-5 text-gray-500 dark:text-gray-400">
      <div class="border-t border-gray-100 pt-3 dark:border-dark-700"><slot /></div>
    </footer>
  </article>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{
  title: string
  scope: string
  to: string
  loading?: boolean
  error?: string
  metrics: {label: string; value: string; detail?: string}[]
}>()
const { t } = useI18n()
</script>
