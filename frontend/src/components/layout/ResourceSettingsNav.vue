<template>
  <section v-if="active" class="mb-6 border-b border-gray-200 dark:border-dark-700" aria-label="Account and resource settings">
    <div class="mb-3">
      <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('compute.resourceSettings') }}</p>
      <p class="mt-1 text-xs text-gray-500">{{ t('compute.resourceHint') }}</p>
    </div>
    <nav class="flex gap-1 overflow-x-auto" :aria-label="t('compute.resourceSettings')">
      <RouterLink v-for="tab in tabs" :key="tab.path" :to="tab.path"
        class="whitespace-nowrap border-b-2 px-4 py-3 text-sm transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
        :class="route.path === tab.path ? 'border-primary-500 font-medium text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-900 dark:hover:text-white'"
        :aria-current="route.path === tab.path ? 'page' : undefined">{{ t(tab.label) }}</RouterLink>
    </nav>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
const route = useRoute()
const { t } = useI18n()
const tabs = [
  {path: '/admin/accounts', label: 'compute.apiAccounts'},
  {path: '/admin/drive', label: 'compute.storageAccounts'},
  {path: '/admin/compute', label: 'compute.computeAccounts'},
  {path: '/admin/proxies', label: 'compute.proxySettings'},
]
const active = computed(() => tabs.some(tab => route.path === tab.path))
</script>
