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

/** Thrown when the browser could not reach Google directly. */
export class DirectUploadError extends Error {}

/**
 * Uploads a file straight from the browser to Google Drive. The server only
 * creates a resumable session, so the file bytes don't pass through it.
 */
export async function uploadDirect(
  userId: number,
  dir: string,
  file: File,
  onProgress?: (fraction: number) => void
): Promise<void> {
  const { data } = await apiClient.post<{ upload_url: string }>(`${base(userId)}/upload-sessions`, {
    path: dir,
    name: file.name,
    size: file.size,
    mime_type: file.type || 'application/octet-stream'
  })
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', data.upload_url)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) onProgress?.(e.loaded / e.total)
    }
    xhr.onload = () =>
      xhr.status === 200 || xhr.status === 201
        ? resolve()
        : reject(new DirectUploadError(`Google Drive returned HTTP ${xhr.status}`))
    xhr.onerror = () => reject(new DirectUploadError('Google Drive is unreachable from this browser'))
    xhr.send(file)
  })
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

export default { status, authorize, disconnect, overview, list, upload, uploadDirect, download, mkdir, remove }
