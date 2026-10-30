import type Icon from '@/components/icons/Icon.vue'

export interface DashboardMetric {
  label: string
  value: string
  detail?: string
  icon: InstanceType<typeof Icon>['$props']['name']
  tone?: 'stat-icon-primary' | 'stat-icon-success' | 'stat-icon-warning' | 'stat-icon-danger'
}
