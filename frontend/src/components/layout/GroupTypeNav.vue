<template>
  <nav v-if="active" class="mb-6 flex gap-1 overflow-x-auto border-b border-gray-200 dark:border-dark-700" :aria-label="t('compute.groupManagement')">
    <RouterLink v-for="tab in tabs" :key="tab.path" :to="tab.path"
      class="whitespace-nowrap border-b-2 px-4 py-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
      :class="route.path === tab.path ? 'border-primary-500 font-medium text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-900 dark:hover:text-white'"
      :aria-current="route.path === tab.path ? 'page' : undefined">{{ t(tab.label) }}</RouterLink>
  </nav>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
const route = useRoute()
const { t } = useI18n()
const tabs = [
  {path: '/admin/groups', label: 'compute.apiGroups'},
  {path: '/admin/groups/storage', label: 'compute.storageGroups'},
  {path: '/admin/groups/compute', label: 'compute.computeGroups'},
]
const active = computed(() => tabs.some(tab => route.path === tab.path))
</script>
