import { useEffect, useRef } from 'react'

export function ConfirmDialog({ Title, Description, ConfirmLabel, Pending, OnConfirm, OnCancel }: {
  Title: string
  Description: string
  ConfirmLabel: string
  Pending: boolean
  OnConfirm: () => void
  OnCancel: () => void
}) {
  const CancelButton = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const Previous = document.activeElement as HTMLElement | null
    CancelButton.current?.focus()
    function HandleKey(Event: KeyboardEvent) {
      if (Event.key === 'Escape') OnCancel()
    }
    window.addEventListener('keydown', HandleKey)
    return () => {
      window.removeEventListener('keydown', HandleKey)
      Previous?.focus?.()
    }
  }, [OnCancel])

  return (
    <div className="dialog-backdrop" onMouseDown={Event => { if (Event.target === Event.currentTarget) OnCancel() }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title" aria-describedby="dialog-description">
        <h2 id="dialog-title">{Title}</h2>
        <p id="dialog-description">{Description}</p>
        <div className="dialog-actions">
          <button ref={CancelButton} className="btn btn-secondary" type="button" onClick={OnCancel}>Cancel</button>
          <button className="btn btn-danger-solid" type="button" onClick={OnConfirm} disabled={Pending}>
            {Pending ? 'Working…' : ConfirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
