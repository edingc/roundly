/**
 * The site administrator's queue.
 *
 * Only one thing lands here so far: requests to remove a course. Nobody owns a
 * course, so nobody can remove one — but removing one destroys every tee, hole,
 * par, and yardage with no undo, so somebody has to decide.
 *
 * The route is gated client-side on user.is_admin purely to avoid showing a
 * page that would only 403. The real check is RequireAdmin on the server, which
 * reads the account's current address rather than trusting a token claim.
 */
import { useCallback, useEffect, useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { RemovalRequest } from '../types'
import { Alert, ConfirmDialog, EmptyState, PageSpinner, Spinner } from '../components/ui'

export default function AdminPage() {
  const { user, loading: authLoading } = useAuth()

  const [requests, setRequests] = useState<RemovalRequest[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<RemovalRequest | null>(null)

  const load = useCallback(async () => {
    try {
      const { requests } = await api.listRemovalRequests()
      setRequests(requests)
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load the queue.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (user?.is_admin) void load()
    else setLoading(false)
  }, [user, load])

  if (authLoading) return <PageSpinner label="Loading" />
  if (!user?.is_admin) return <Navigate to="/courses" replace />

  async function decline(request: RemovalRequest) {
    setBusyId(request.id)
    setError(null)
    try {
      await api.resolveRemovalRequest(request.id, 'declined')
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not decline that request.')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Administration</h1>
        <p className="text-sm text-slate-600 dark:text-slate-400">
          Requests to remove a course from the shared directory.
        </p>
      </div>

      {error && <Alert>{error}</Alert>}

      {loading ? (
        <PageSpinner label="Loading requests" />
      ) : requests.length === 0 ? (
        <EmptyState
          title="Nothing waiting"
          description="When someone asks for a course to be removed, it appears here for you to decide."
        />
      ) : (
        <ul className="space-y-3">
          {requests.map((request) => (
            <li key={request.id} className="card space-y-3 p-5">
              <div>
                <h2 className="font-semibold">
                  {request.course_id ? (
                    <Link to={`/courses/${request.course_id}`} className="hover:underline">
                      {request.course_name}
                    </Link>
                  ) : (
                    request.course_name
                  )}
                </h2>
                <p className="text-xs text-slate-500 dark:text-slate-400">
                  Asked by {request.requested_by_name ?? 'a deleted account'} on{' '}
                  {formatDate(request.created_at)}
                </p>
              </div>

              <p className="text-sm whitespace-pre-line text-slate-700 dark:text-slate-300">
                {request.reason}
              </p>

              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  className="btn-secondary !min-h-0 !px-3 !py-1.5 !text-sm"
                  disabled={busyId === request.id}
                  onClick={() => void decline(request)}
                >
                  {busyId === request.id ? <Spinner label="Working" /> : 'Keep the course'}
                </button>
                <button
                  type="button"
                  className="btn-danger !min-h-0 !px-3 !py-1.5 !text-sm"
                  disabled={busyId === request.id}
                  onClick={() => setConfirming(request)}
                >
                  Remove it
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {confirming && (
        <ConfirmDialog
          title={`Remove ${confirming.course_name}?`}
          confirmLabel="Remove course"
          message={
            <>
              This deletes the course along with its tees, holes, and every par and yardage
              anybody has entered. It cannot be undone, and other players may have it set as
              their home course.
            </>
          }
          onCancel={() => setConfirming(null)}
          onConfirm={async () => {
            await api.resolveRemovalRequest(confirming.id, 'removed')
            setConfirming(null)
            await load()
          }}
        />
      )}
    </div>
  )
}

function formatDate(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
