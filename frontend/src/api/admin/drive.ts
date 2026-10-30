/**
 * Drive storage operations: the Google accounts that make up the cloud drive
 * pool, per-user usage and quotas, and access to any user's files.
 */

import { apiClient } from '../client'
import { driveClient, type DriveClient, type DriveUsage } from '../drive'

export interface DriveConfigCheck {
  key: 'encryption' | 'oauth_client' | 'redirect_uri' | 'storage' | string
  ok: boolean
  detail: string
}

export interface DriveStorageAccount {
  id: number
  google_email: string
  enabled: boolean
  quota_limit: number | null
  quota_usage: number | null
  quota_checked_at: string | null
  last_error: string
  file_count: number
  stored_bytes: number
  reserved_bytes: number
  created_at: string
  updated_at: string
}

export interface DrivePoolSummary {
  accounts: number
  usable_accounts: number
  capacity_bytes: number
  free_bytes: number
  unlimited: boolean
  stored_bytes: number
  file_count: number
}

export interface DriveUserUsage extends DriveUsage {
  user_id: number
  email: string
}

export interface DriveAdminStatus {
  enabled: boolean
  reason: string
  redirect_uri: string
  max_bytes: number
  checks: DriveConfigCheck[]
  pool: DrivePoolSummary
  accounts: DriveStorageAccount[] | null
  users: DriveUserUsage[] | null
}

export async function status(): Promise<DriveAdminStatus> {
  const { data } = await apiClient.get<DriveAdminStatus>('/admin/drive')
  return data
}

/** Returns a Google consent URL that adds (accountId 0) or renews a storage account. */
export async function authorize(accountId = 0): Promise<string> {
  const { data } = await apiClient.post<{ auth_url: string }>('/admin/drive/accounts/authorize', { account_id: accountId })
  return data.auth_url
}

export async function refresh(accountId: number): Promise<DriveStorageAccount> {
  const { data } = await apiClient.post<DriveStorageAccount>(`/admin/drive/accounts/${accountId}/refresh`)
  return data
}

export async function setEnabled(accountId: number, enabled: boolean): Promise<void> {
  await apiClient.put(`/admin/drive/accounts/${accountId}`, { enabled })
}

export async function remove(accountId: number): Promise<void> {
  await apiClient.delete(`/admin/drive/accounts/${accountId}`)
}

export async function setQuota(userId: number, quotaBytes: number | null): Promise<void> {
  await apiClient.put(`/admin/drive/users/${userId}/quota`, { quota_bytes: quotaBytes })
}

export function userDrive(userId: number): DriveClient {
  return driveClient(`/admin/drive/users/${userId}`)
}

export default { status, authorize, refresh, setEnabled, remove, setQuota, userDrive }
