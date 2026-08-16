import { useRef, useState } from 'react'
import { ApiError, api } from '../lib/api'
import type { ImportSummary } from '../types'
import { Alert, DownloadIcon, SegmentedControl, Spinner, UploadIcon } from './ui'

type Format = 'json' | 'csv'

const FORMAT_OPTIONS: Array<{ value: Format; label: string }> = [
  { value: 'json', label: 'JSON' },
  { value: 'csv', label: 'CSV' },
]

/**
 * Download my data, and put it back.
 *
 * The download is built client-side from an authenticated fetch rather than a
 * plain link, for the same reason the course export is: the endpoint needs a
 * bearer header and an <a href> cannot send one.
 */
export function DataSection({ onImported }: { onImported: () => void }) {
  const [format, setFormat] = useState<Format>('json')
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)

  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState<string | null>(null)
  const [summary, setSummary] = useState<ImportSummary | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  function download(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob)
    try {
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      a.click()
    } finally {
      URL.revokeObjectURL(url)
    }
  }

  async function handleExport() {
    setExporting(true)
    setExportError(null)
    try {
      const stamp = new Date().toISOString().slice(0, 10)
      if (format === 'csv') {
        download(await api.exportAccountCsv(), `roundly-export-${stamp}.zip`)
      } else {
        const data = await api.exportAccount()
        download(
          new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
          `roundly-export-${stamp}.json`,
        )
      }
    } catch (err) {
      setExportError(err instanceof ApiError ? err.message : 'Could not export your data.')
    } finally {
      setExporting(false)
    }
  }

  async function handleImportFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = '' // lets the same file be picked again after an error
    if (!file) return

    setImporting(true)
    setImportError(null)
    setSummary(null)
    try {
      let payload: unknown
      try {
        payload = JSON.parse(await file.text())
      } catch {
        throw new Error('That file is not valid JSON.')
      }
      setSummary(await api.importAccount(payload))
      onImported()
    } catch (err) {
      setImportError(
        err instanceof ApiError || err instanceof Error ? err.message : 'Could not restore that file.',
      )
    } finally {
      setImporting(false)
    }
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Download my data</h2>

      <div className="card space-y-5 p-5">
        <p className="text-sm text-slate-600 dark:text-slate-400">
          A copy of everything on your account: your profile and photo, every club in your bag
          including retired ones, and every course you have added with its tees, holes, pars, and
          yardages. Your password and sign-in tokens are never included.
        </p>

        {exportError && <Alert>{exportError}</Alert>}

        <div className="space-y-2">
          <span className="label">Format</span>
          <SegmentedControl label="Export format" value={format} options={FORMAT_OPTIONS} onChange={setFormat} />
          <p className="text-sm text-slate-500 dark:text-slate-400">
            {format === 'json'
              ? 'One file, and the only format you can restore from.'
              : 'A ZIP of spreadsheet-friendly tables. Good for reading, but it cannot be restored.'}
          </p>
        </div>

        <button type="button" className="btn-primary" disabled={exporting} onClick={() => void handleExport()}>
          {exporting ? (
            <Spinner label="Preparing" />
          ) : (
            <>
              <DownloadIcon className="size-4" />
              Download my data
            </>
          )}
        </button>
      </div>

      <div className="card space-y-4 p-5">
        <div>
          <h3 className="font-semibold">Restore from a backup</h3>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Adds anything missing from a JSON export. Nothing is overwritten and nothing is
            deleted, so a club or course you already have is skipped rather than duplicated.
          </p>
        </div>

        {importError && <Alert>{importError}</Alert>}
        {summary && <ImportReport summary={summary} />}

        <input
          ref={fileInputRef}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(e) => void handleImportFile(e)}
        />
        <button
          type="button"
          className="btn-secondary"
          disabled={importing}
          onClick={() => fileInputRef.current?.click()}
        >
          {importing ? (
            <Spinner label="Restoring" />
          ) : (
            <>
              <UploadIcon className="size-4" />
              Choose a backup file
            </>
          )}
        </button>
      </div>
    </section>
  )
}

/** Reports exactly what a restore did, since "skipped" and "worked" look alike. */
function ImportReport({ summary }: { summary: ImportSummary }) {
  const line = (label: string, c: ImportSummary['clubs']) =>
    `${label}: ${c.imported} added, ${c.skipped} already there${c.failed > 0 ? `, ${c.failed} failed` : ''}`

  const filled = summary.profile.fields_filled ?? []

  return (
    <Alert tone="success">
      <div className="space-y-1">
        <p className="font-medium">Restore complete.</p>
        <p>{line('Clubs', summary.clubs)}</p>
        <p>{line('Courses', summary.courses)}</p>
        <p>{line('Rounds', summary.rounds)}</p>
        {filled.length > 0 && <p>Profile fields filled in: {filled.join(', ')}.</p>}
        {(summary.warnings ?? []).map((w) => (
          <p key={w}>{w}</p>
        ))}
      </div>
    </Alert>
  )
}
