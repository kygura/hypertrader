import { useState } from 'react'
import type { PortfolioImport } from '../../lib/types'
import { validateImport } from '../../lib/branches-store'

interface Props {
  onCancel: () => void
  onSubmit: (data: PortfolioImport) => void
}

export function ImportDialog({ onCancel, onSubmit }: Props) {
  const [text, setText] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [preview, setPreview] = useState<PortfolioImport | null>(null)
  const [drag, setDrag] = useState(false)

  const validate = (raw: string) => {
    setText(raw)
    setError(null)
    setPreview(null)
    if (!raw.trim()) return
    try {
      const json = JSON.parse(raw)
      const res = validateImport(json)
      if (!res.ok || !res.data) {
        setError(res.error ?? 'Invalid')
        return
      }
      setPreview(res.data)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const onDrop = async (e: React.DragEvent) => {
    e.preventDefault()
    setDrag(false)
    const file = e.dataTransfer.files?.[0]
    if (!file) return
    const t = await file.text()
    validate(t)
  }

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const t = await file.text()
    validate(t)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.5)' }}
      onClick={onCancel}
    >
      <div
        className="bg-panel border border-border w-[520px] max-h-[90vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-panel-alt">
          <span className="label">Import Portfolio</span>
          <button onClick={onCancel} className="text-text-secondary hover:text-text-primary text-[14px]">
            ×
          </button>
        </div>
        <div className="p-3 flex-1 overflow-auto">
          <div
            onDragOver={(e) => {
              e.preventDefault()
              setDrag(true)
            }}
            onDragLeave={() => setDrag(false)}
            onDrop={onDrop}
            className={`border border-dashed p-4 text-center mb-2 ${
              drag ? 'border-amber bg-elevated' : 'border-border bg-panel-alt'
            }`}
          >
            <div className="text-[11px] uppercase tracking-wider text-text-secondary">
              Drop JSON file here
            </div>
            <input
              type="file"
              accept="application/json,.json"
              onChange={onFile}
              className="block mx-auto mt-2 text-[11px]"
            />
          </div>
          <div className="label mb-1">Or paste JSON</div>
          <textarea
            value={text}
            onChange={(e) => validate(e.target.value)}
            placeholder='{ "name": "my-portfolio", "startingBalance": 10000, "positions": [...] }'
            spellCheck={false}
            className="w-full h-32 font-mono text-[11px] bg-panel-alt border border-border p-2 text-text-primary"
          />
          {error && (
            <div className="mt-2 text-[11px] text-red font-mono">⚠ {error}</div>
          )}
          {preview && (
            <div className="mt-2 border border-border bg-panel-alt p-2">
              <div className="label mb-1">Preview</div>
              <div className="text-[11px] font-mono text-text-primary">
                <div>NAME · {preview.name}</div>
                <div>STARTING BALANCE · ${preview.startingBalance.toLocaleString()}</div>
                <div>POSITIONS · {preview.positions.length}</div>
                <div>PENDING ORDERS · {preview.pendingOrders?.length ?? 0}</div>
              </div>
            </div>
          )}
        </div>
        <div className="px-3 py-2 flex gap-2 justify-end border-t border-border bg-panel-alt">
          <button
            onClick={onCancel}
            className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            onClick={() => preview && onSubmit(preview)}
            disabled={!preview}
            className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border bg-elevated text-text-primary disabled:opacity-50"
          >
            Import
          </button>
        </div>
      </div>
    </div>
  )
}
