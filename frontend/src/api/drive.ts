/**
 * Cloud drive (网盘) API. The same file operations serve the signed-in user
 * (/user/drive) and administrators acting on a user (/admin/drive/users/:id).
 */

import { apiClient } from './client'

export interface DriveUsage {
  used_bytes: number
  reserved_bytes: number
  file_count: number
  folder_count: number
  quota_bytes: number | null
}

export interface DriveOverview extends DriveUsage {
  enabled: boolean
  /** What can still be stored; null means no known limit. */
  available_bytes: number | null
  max_relay_bytes: number
}

export interface DriveEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  mime_type: string
  modified: string
}

/** Thrown when the browser could not reach Google directly. */
export class DirectUploadError extends Error {}

export interface DriveClient {
  overview(): Promise<{ overview: DriveOverview; webdav_path: string }>
  list(path: string): Promise<DriveEntry[]>
  upload(dir: string, file: File, onProgress?: (fraction: number) => void): Promise<'direct' | 'relay'>
  download(path: string): Promise<Blob>
  mkdir(path: string): Promise<void>
  move(from: string, to: string): Promise<void>
  remove(path: string): Promise<void>
}

export function driveClient(base: string): DriveClient {
  async function uploadDirect(dir: string, file: File, onProgress?: (fraction: number) => void) {
    const { data } = await apiClient.post<{ upload_id: number; upload_url: string }>(`${base}/upload-sessions`, {
      path: dir,
      name: file.name,
      size: file.size,
      mime_type: file.type || 'application/octet-stream'
    })
    try {
      await new Promise<void>((resolve, reject) => {
        const xhr = new XMLHttpRequest()
        xhr.open('PUT', data.upload_url)
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable && e.total > 0) onProgress?.(e.loaded / e.total)
        }
        xhr.onload = () =>
          xhr.status === 200 || xhr.status === 201
            ? resolve()
            : reject(new DirectUploadError(`Google returned HTTP ${xhr.status}`))
        xhr.onerror = () => reject(new DirectUploadError('storage is unreachable from this browser'))
        xhr.send(file)
      })
    } catch (err) {
      // Release the reserved space before falling back or failing.
      await apiClient.delete(`${base}/uploads/${data.upload_id}`).catch(() => undefined)
      throw err
    }
    await apiClient.post(`${base}/uploads/${data.upload_id}/complete`)
  }

  async function uploadRelay(dir: string, file: File) {
    const form = new FormData()
    form.append('file', file, file.name)
    await apiClient.post(`${base}/files`, form, { params: { path: dir }, timeout: 0 })
  }

  return {
    async overview() {
      const { data } = await apiClient.get<{ overview: DriveOverview; webdav_path: string }>(base)
      return data
    },
    async list(path) {
      const { data } = await apiClient.get<{ items: DriveEntry[] | null }>(`${base}/files`, { params: { path } })
      return data.items ?? []
    },
    async upload(dir, file, onProgress) {
      try {
        await uploadDirect(dir, file, onProgress)
        return 'direct'
      } catch (err) {
        // Browsers that cannot reach Google directly upload through this server.
        if (!(err instanceof DirectUploadError)) throw err
        await uploadRelay(dir, file)
        return 'relay'
      }
    },
    async download(path) {
      const { data } = await apiClient.get<Blob>(`${base}/download`, { params: { path }, responseType: 'blob', timeout: 0 })
      return data
    },
    async mkdir(path) {
      await apiClient.post(`${base}/folders`, { path })
    },
    async move(from, to) {
      await apiClient.post(`${base}/move`, { from, to })
    },
    async remove(path) {
      await apiClient.delete(`${base}/files`, { params: { path } })
    }
  }
}

export const myDrive = driveClient('/user/drive')

export function formatBytes(value: number | null | undefined): string {
  if (value == null) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let n = value
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}
