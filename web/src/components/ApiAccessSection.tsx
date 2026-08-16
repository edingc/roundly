import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../lib/api'
import type { ApiKey, CreatedApiKey } from '../types'
import { Alert, ConfirmDialog, Field, PlusIcon, Spinner, WarningIcon, cx } from './ui'

const EXPIRY_OPTIONS = [
  { value: 0, label: 'Never' },
  { value: 30, label: '30 days' },
  { value: 90, label: '90 days' },
  { value: 365, label: '1 year' },
]

/**
 * Create and manage read-only API keys.
 *
 * The secret appears exactly once, in the response to creation. Nothing in this
 * component stores it anywhere, and the list endpoint has no token field at all
 * — so once the banner is dismissed the key really is unrecoverable, which is
 * what the copy promises.
 */
export function ApiAccessSection() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [creating, setCreating] = useState(false)
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState('')
  const [expiresInDays, setExpiresInDays] = useState(0)
  const [formErrors, setFormErrors] = useState<Record<string, string>>({})

  const [created, setCreated] = useState<CreatedApiKey | null>(null)
  const [copied, setCopied] = useState(false)
  const [revoking, setRevoking] = useState<ApiKey | null>(null)

  const load = useCallback(async () => {
    try {
      const { keys } = await api.listApiKeys()
      setKeys(keys)
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load your API keys.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function handleCreate(event: React.FormEvent) {
    event.preventDefault()
    setCreating(true)
    setFormErrors({})
    try {
      setCreated(await api.createApiKey(name, expiresInDays))
      setCopied(false)
      setName('')
      setExpiresInDays(0)
      setShowForm(false)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.isValidation) setFormErrors(err.fields)
      else setFormErrors({ name: err instanceof ApiError ? err.message : 'Could not create a key.' })
    } finally {
      setCreating(false)
    }
  }

  async function handleCopy(token: string) {
    try {
      await navigator.clipboard.writeText(token)
      setCopied(true)
    } catch {
      // Clipboard access can be refused; the key is on screen to select.
      setCopied(false)
    }
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">API access</h2>

      <div className="card space-y-5 p-5">
        <div>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Use a personal API key to read your golf data from scripts and other tools.
          </p>
          <p className="mt-2 inline-flex items-center gap-2 rounded-lg bg-brand-50 px-3 py-2 text-sm text-brand-900 dark:bg-brand-950/60 dark:text-brand-200">
            <span className="font-semibold">Permissions:</span> Read-only
          </p>
          <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">
            A key can read your profile, your bag, and the course directory. It cannot create,
            change, or delete anything, cannot reach your account settings, and cannot see anyone
            else's data.
          </p>
        </div>

        {error && <Alert>{error}</Alert>}

        {created && (
          <div className="rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/60">
            <p className="flex items-start gap-2 text-sm font-medium text-amber-900 dark:text-amber-100">
              <WarningIcon className="mt-0.5 size-4 shrink-0" />
              Save this key somewhere secure. For security, you will not be able to view it again.
            </p>
            <code className="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-sm break-all text-slate-900 dark:bg-slate-900 dark:text-slate-100">
              {created.token}
            </code>
            <div className="mt-3 flex flex-wrap gap-2">
              <button
                type="button"
                className="btn-secondary !min-h-0 !px-3 !py-1.5 !text-sm"
                onClick={() => void handleCopy(created.token)}
              >
                {copied ? 'Copied' : 'Copy key'}
              </button>
              <button
                type="button"
                className="btn-secondary !min-h-0 !px-3 !py-1.5 !text-sm"
                onClick={() => setCreated(null)}
              >
                Done
              </button>
            </div>
            <p className="mt-3 text-xs text-amber-900 dark:text-amber-200">
              Treat it like a password. Anyone holding it can read your golf data.
            </p>
          </div>
        )}

        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>
        ) : keys.length === 0 ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">You have no API keys.</p>
        ) : (
          <ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
            {keys.map((key) => (
              <li key={key.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="font-medium">{key.name}</p>
                  <p className="font-mono text-xs text-slate-500 dark:text-slate-400">
                    {key.key_prefix}
                    {'…'}
                  </p>
                  <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                    Read-only · Created {formatDate(key.created_at)} · Last used{' '}
                    {key.last_used_at ? formatDate(key.last_used_at) : 'never'}
                    {key.expires_at && ` · Expires ${formatDate(key.expires_at)}`}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setRevoking(key)}
                  className={cx(
                    'btn-secondary !min-h-0 !px-2.5 !py-1.5 !text-xs',
                    '!text-red-700 hover:!bg-red-50 dark:!text-red-300 dark:hover:!bg-red-950/50',
                  )}
                >
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        )}

        {showForm ? (
          <form onSubmit={handleCreate} className="space-y-4 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
            <Field
              id="key-name"
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              error={formErrors.name}
              hint="What will use this key, so you can tell your keys apart later."
              placeholder="Stats script"
              required
              maxLength={60}
            />
            <div>
              <label htmlFor="key-expiry" className="label">
                Expires
              </label>
              <select
                id="key-expiry"
                value={expiresInDays}
                onChange={(e) => setExpiresInDays(Number(e.target.value))}
                className={cx('input', formErrors.expires_in_days && 'input-error')}
              >
                {EXPIRY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              {formErrors.expires_in_days && <p className="field-error">{formErrors.expires_in_days}</p>}
            </div>
            <div className="flex gap-2">
              <button type="button" className="btn-secondary" onClick={() => setShowForm(false)}>
                Cancel
              </button>
              <button type="submit" className="btn-primary" disabled={creating}>
                {creating ? <Spinner label="Creating" /> : 'Create API key'}
              </button>
            </div>
          </form>
        ) : (
          <button type="button" className="btn-primary" onClick={() => setShowForm(true)}>
            <PlusIcon className="size-4" />
            Create API key
          </button>
        )}
      </div>

      {revoking && (
        <ConfirmDialog
          title={`Revoke ${revoking.name}?`}
          confirmLabel="Revoke"
          message={
            <>
              Anything using this key stops working immediately. This cannot be undone — you would
              need to create a new key and update whatever was using it.
            </>
          }
          onCancel={() => setRevoking(null)}
          onConfirm={async () => {
            await api.revokeApiKey(revoking.id)
            setRevoking(null)
            await load()
          }}
        />
      )}
    </section>
  )
}

function formatDate(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
