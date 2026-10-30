<template>
  <article class="min-w-0 space-y-4" :aria-busy="loading">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-white shadow-sm dark:bg-dark-800"><slot name="icon" /></span>
        <div>
          <h3 class="font-semibold text-gray-900 dark:text-white">{{ title }}</h3>
          <p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">{{ scope }}</p>
        </div>
      </div>
      <RouterLink :to="to" class="btn btn-secondary btn-sm">{{ t('compute.dashboardOpen') }}<Icon name="chevronRight" size="sm" class="ml-1" /></RouterLink>
    </header>
    <p v-if="error" role="alert" class="card p-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <p v-else-if="!metrics.length" class="card p-4 text-sm text-gray-500">{{ t('common.loading') }}</p>
    <dl v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div v-for="item in metrics" :key="item.label" class="stat-card min-w-0" data-testid="metric-block">
        <span class="stat-icon shrink-0" :class="item.tone || 'stat-icon-primary'" aria-hidden="true">
          <Icon :name="item.icon" size="lg" />
        </span>
        <div class="min-w-0">
          <dt class="text-sm leading-5 text-gray-500 dark:text-gray-400">{{ item.label }}</dt>
          <dd class="mt-1 break-words text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</dd>
          <dd v-if="item.detail" class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ item.detail }}</dd>
        </div>
      </div>
    </dl>
    <footer v-if="$slots.default" class="text-xs leading-5 text-gray-500 dark:text-gray-400"><slot /></footer>
    <div v-if="$slots.content" class="space-y-6 pt-2"><slot name="content" /></div>
  </article>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { DashboardMetric } from './types'
defineProps<{
  title: string
  scope: string
  to: string
  loading?: boolean
  error?: string
  metrics: DashboardMetric[]
}>()
const { t } = useI18n()
</script>
