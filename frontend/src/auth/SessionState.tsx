import { Icon } from '../ui/Icon'

export function SessionSplash() {
  return (
    <div className="splash">
      <p className="state" role="status"><span className="spinner" aria-hidden="true" />Loading session…</p>
    </div>
  )
}

export function SessionError({ Message, OnRetry }: { Message: string; OnRetry: () => void }) {
  return (
    <main className="splash">
      <div className="splash-inner">
        <span className="empty-icon"><Icon Name="alert" Size={20} /></span>
        <p role="alert">{Message}</p>
        <button className="btn btn-secondary" type="button" onClick={OnRetry}>Try again</button>
      </div>
    </main>
  )
}
