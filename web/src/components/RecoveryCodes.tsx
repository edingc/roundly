/**
 * The one screen a recovery sheet is ever shown on.
 *
 * The codes are stored hashed, so this is genuinely the only moment they exist
 * in readable form. Everything here is in service of that: the panel is loud,
 * it does not disappear on its own, and dismissing it takes a deliberate click
 * that says the user has saved them.
 */
import { useState } from 'react'
import { Alert, DownloadIcon, cx } from './ui'

export function RecoveryCodesPanel({
  codes,
  onDone,
}: {
  codes: string[]
  onDone: () => void
}) {
  const [copied, setCopied] = useState(false)

  const asText =
    'Roundly recovery codes\n' +
    'Each code works once. Keep them somewhere you can reach without your email.\n\n' +
    codes.join('\n') +
    '\n'

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(asText)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard access can be refused outright. The codes are on screen and
      // the download still works, so this needs no error of its own.
    }
  }

  function handleDownload() {
    const url = URL.createObjectURL(new Blob([asText], { type: 'text/plain' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'roundly-recovery-codes.txt'
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/60">
      <div>
        <h4 className="font-semibold text-amber-900 dark:text-amber-100">
          Save your recovery codes
        </h4>
        <p className="mt-1 text-sm text-amber-900/90 dark:text-amber-100/90">
          These are the way back into your account if you lose access to your email. Each one
          works once. <strong>This is the only time they will be shown</strong> — they are stored
          hashed, so nobody, including this server, can read them back to you.
        </p>
      </div>

      <ul className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-md bg-white/70 p-3 font-mono text-sm dark:bg-black/30">
        {codes.map((code) => (
          <li key={code} className="tracking-wider">
            {code}
          </li>
        ))}
      </ul>

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
          onClick={handleDownload}
        >
          <DownloadIcon className="size-4" />
          Download
        </button>
        <button
          type="button"
          className={cx('btn-secondary !min-h-0 !px-3 !py-2 !text-sm', copied && 'opacity-70')}
          onClick={() => void handleCopy()}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button type="button" className="btn-primary !min-h-0 !px-3 !py-2 !text-sm" onClick={onDone}>
          I have saved them
        </button>
      </div>
    </div>
  )
}

/**
 * Warns as the sheet runs down.
 *
 * The moment worth catching is *before* the last code is spent, because the
 * point of having any is not needing your email to get a new set — and once
 * they are gone, that is exactly what it takes.
 */
export function RecoveryCodesStatus({ remaining }: { remaining: number }) {
  if (remaining === 0) {
    return (
      <Alert tone="warning">
        You have no recovery codes left. Generate a new sheet — without one, losing access to
        your email means losing the account.
      </Alert>
    )
  }
  if (remaining <= 3) {
    return (
      <Alert tone="warning">
        {remaining} recovery {remaining === 1 ? 'code' : 'codes'} left. Generate a fresh sheet
        while you still can.
      </Alert>
    )
  }
  return (
    <p className="text-sm text-slate-600 dark:text-slate-400">
      {remaining} recovery codes left.
    </p>
  )
}
