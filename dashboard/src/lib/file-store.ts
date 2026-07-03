// JSON file persistence for branch sets.
// Uses File System Access API when available, falls back to download+upload.

import type { Branch } from './types'

const DEFAULT_FILENAME = 'hypertrade-branches.json'

declare global {
  interface Window {
    showSaveFilePicker?: (opts?: {
      suggestedName?: string
      types?: Array<{ description: string; accept: Record<string, string[]> }>
    }) => Promise<FileSystemFileHandle>
    showOpenFilePicker?: (opts?: {
      types?: Array<{ description: string; accept: Record<string, string[]> }>
      multiple?: boolean
    }) => Promise<FileSystemFileHandle[]>
  }
}

interface FileSystemFileHandle {
  createWritable(): Promise<FileSystemWritableFileStream>
  getFile(): Promise<File>
}

interface FileSystemWritableFileStream extends WritableStream {
  write(data: string | Blob | ArrayBuffer): Promise<void>
  close(): Promise<void>
}

export function serializeBranches(branches: Branch[]): string {
  return JSON.stringify(branches, null, 2)
}

export function deserializeBranches(json: string): Branch[] {
  const parsed = JSON.parse(json) as unknown
  if (!Array.isArray(parsed) || parsed.length === 0) {
    throw new Error('Not a valid branch set: must be a non-empty array')
  }
  for (let i = 0; i < parsed.length; i++) {
    const b = parsed[i] as Record<string, unknown>
    if (typeof b.id !== 'string') throw new Error(`Branch ${i}: missing id`)
    if (typeof b.name !== 'string') throw new Error(`Branch ${i}: missing name`)
    if (!Array.isArray(b.positions)) throw new Error(`Branch ${i}: missing positions`)
    if (typeof b.startingBalance !== 'number') throw new Error(`Branch ${i}: missing startingBalance`)
  }
  return parsed as Branch[]
}

export function isFileSystemApiAvailable(): boolean {
  return typeof window !== 'undefined' && !!window.showSaveFilePicker && !!window.showOpenFilePicker
}

export async function saveWithFilePicker(branches: Branch[]): Promise<boolean> {
  if (!isFileSystemApiAvailable()) return false
  try {
    const handle = await window.showSaveFilePicker!({
      suggestedName: DEFAULT_FILENAME,
      types: [
        {
          description: 'JSON',
          accept: { 'application/json': ['.json'] },
        },
      ],
    })
    const writable = await handle.createWritable()
    await writable.write(serializeBranches(branches))
    await writable.close()
    return true
  } catch {
    return false
  }
}

export function downloadBranchFile(branches: Branch[], filename = DEFAULT_FILENAME): void {
  const blob = new Blob([serializeBranches(branches)], {
    type: 'application/json',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

export async function loadFromFilePicker(): Promise<Branch[] | null> {
  if (!isFileSystemApiAvailable()) return null
  try {
    const [handle] = await window.showOpenFilePicker!({
      types: [
        {
          description: 'JSON',
          accept: { 'application/json': ['.json'] },
        },
      ],
      multiple: false,
    })
    const file = await handle.getFile()
    const text = await file.text()
    return deserializeBranches(text)
  } catch {
    return null
  }
}

export function triggerFileLoad(onLoad: (branches: Branch[]) => void): void {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = '.json,application/json'
  input.onchange = () => {
    const file = input.files?.[0]
    if (!file) return
    file.text().then((text) => {
      try {
        const branches = deserializeBranches(text)
        onLoad(branches)
      } catch (e) {
        alert('Failed to load: ' + (e as Error).message)
      }
    })
  }
  input.click()
}
