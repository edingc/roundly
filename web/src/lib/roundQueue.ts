/**
 * The local-first half of a live round.
 *
 * Golf courses are where phones lose signal, and a round that cannot save the
 * front nine is worse than useless. So a hole is written here first and pushed
 * afterwards: the screen never waits on the network, and a hole entered in a
 * dead zone is still there when the signal comes back.
 *
 * What makes this safe rather than merely optimistic is that the server's hole
 * endpoint is an idempotent upsert keyed on (round, hole number). A queued
 * write can be replayed any number of times and the result is the same hole,
 * so this never has to reason about whether a request that timed out actually
 * landed. It just sends it again.
 *
 * The queue is a map rather than a log, keyed by round and hole, and that is
 * the second half of the same idea: re-entering a hole replaces what was
 * queued for it. Nobody wants the third putt they corrected twice to be
 * replayed in order.
 */
import { api } from './api'
import type { Round, RoundHolePayload } from '../types'

const QUEUE_KEY = 'roundly.round_queue'
const CACHE_KEY = 'roundly.round_cache'

type Queue = Record<string, RoundHolePayload>
type Cache = Record<string, Round>

function entryKey(roundID: string, holeNumber: number): string {
  return `${roundID}:${holeNumber}`
}

function read<T>(key: string): T {
  try {
    return JSON.parse(localStorage.getItem(key) ?? '{}') as T
  } catch {
    // Corrupt storage is not worth crashing a round over. Starting from empty
    // loses at most the holes that had not yet been pushed.
    return {} as T
  }
}

function write(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Private browsing, or a full quota. The round still works for as long as
    // the tab is open; it just stops surviving a reload.
  }
}

/** Remembers a round so the screen can be drawn without the network. */
export function cacheRound(round: Round): void {
  const cache = read<Cache>(CACHE_KEY)
  cache[round.id] = round
  write(CACHE_KEY, cache)
}

export function cachedRound(roundID: string): Round | null {
  return read<Cache>(CACHE_KEY)[roundID] ?? null
}

/** Drops a round's cache and any queued holes, once it is finished with. */
export function forgetRound(roundID: string): void {
  const cache = read<Cache>(CACHE_KEY)
  delete cache[roundID]
  write(CACHE_KEY, cache)

  const queue = read<Queue>(QUEUE_KEY)
  for (const key of Object.keys(queue)) {
    if (key.startsWith(`${roundID}:`)) delete queue[key]
  }
  write(QUEUE_KEY, queue)
}

/** Queues a hole for the server. Replaces whatever was queued for that hole. */
export function enqueueHole(roundID: string, hole: RoundHolePayload): void {
  const queue = read<Queue>(QUEUE_KEY)
  queue[entryKey(roundID, hole.hole_number)] = hole
  write(QUEUE_KEY, queue)
}

/** How many holes are waiting to be pushed, for the whole app or one round. */
export function pendingCount(roundID?: string): number {
  const queue = read<Queue>(QUEUE_KEY)
  const keys = Object.keys(queue)
  if (!roundID) return keys.length
  return keys.filter((k) => k.startsWith(`${roundID}:`)).length
}

/**
 * Pushes everything queued, oldest round first.
 *
 * Returns the last round the server sent back, so a caller that just flushed
 * its own round can adopt the authoritative copy. A failure leaves the entry
 * queued and stops: there is no point hammering a connection that is not there,
 * and the next attempt will pick up where this one left off.
 */
export async function flushQueue(roundID?: string): Promise<Round | null> {
  const queue = read<Queue>(QUEUE_KEY)
  const keys = Object.keys(queue).filter((k) => !roundID || k.startsWith(`${roundID}:`))
  let last: Round | null = null

  for (const key of keys) {
    const hole = queue[key]
    const id = key.slice(0, key.lastIndexOf(':'))
    try {
      last = await api.saveRoundHole(id, hole)
    } catch {
      // Still offline, or the server refused. Either way the hole stays queued.
      return last
    }
    // Re-read rather than mutating the copy taken above: the player may have
    // entered another hole while this request was in flight, and that write
    // must not be thrown away by this one's bookkeeping.
    const current = read<Queue>(QUEUE_KEY)
    if (JSON.stringify(current[key]) === JSON.stringify(hole)) {
      delete current[key]
      write(QUEUE_KEY, current)
    }
  }
  return last
}

/** True when the browser believes it can reach the network. */
export function isOnline(): boolean {
  return typeof navigator === 'undefined' || navigator.onLine
}
