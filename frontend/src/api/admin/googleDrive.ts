/**
 * Admin Google Drive API: per-user Drive connections and file management.
 */

import { apiClient } from '../client'

export interface DriveUserStatus {
  user_id: number
  email: string
  connected: boolean
  label: string
  updated_at?: string
}

export interface DriveStatus {
  enabled: boolean
  reason: string
  max_bytes: number
  users: DriveUserStatus[]
}

export interface DriveOverview {
  google_account: string
  limit: number | null
  usage: number | null
  usage_in_drive: number | null
}

export interface DriveEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  modified: string
}

const base = (userId: number) => `/admin/google-drive/users/${userId}`

export async function status(): Promise<DriveStatus> {
  const { data } = await apiClient.get<DriveStatus>('/admin/google-drive')
  return data
}

export async function authorize(userId: number): Promise<string> {
  const { data } = await apiClient.post<{ auth_url: string }>(`${base(userId)}/authorize`)
  return data.auth_url
}

export async function disconnect(userId: number): Promise<void> {
  await apiClient.delete(base(userId))
}

export async function overview(userId: number): Promise<DriveOverview> {
  const { data } = await apiClient.get<DriveOverview>(`${base(userId)}/overview`)
  return data
}

export async function list(userId: number, path: string): Promise<DriveEntry[]> {
  const { data } = await apiClient.get<{ items: DriveEntry[] | null }>(`${base(userId)}/files`, {
    params: { path }
  })
  return data.items ?? []
}

export async function upload(userId: number, dir: string, file: File): Promise<void> {
  const form = new FormData()
  form.append('file', file, file.name)
  await apiClient.post(`${base(userId)}/files`, form, { params: { path: dir }, timeout: 0 })
}

export async function download(userId: number, path: string): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`${base(userId)}/download`, {
    params: { path },
    responseType: 'blob',
    timeout: 0
  })
  return data
}

export async function mkdir(userId: number, path: string): Promise<void> {
  await apiClient.post(`${base(userId)}/folders`, { path })
}

export async function remove(userId: number, path: string): Promise<void> {
  await apiClient.delete(`${base(userId)}/files`, { params: { path } })
}

export default { status, authorize, disconnect, overview, list, upload, download, mkdir, remove }
