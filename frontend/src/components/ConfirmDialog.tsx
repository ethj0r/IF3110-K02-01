import { useEffect } from 'react'

export function ConfirmDialog({ Title, Description, ConfirmLabel, Pending, OnConfirm, OnCancel }: {
  Title: string
  Description: string
  ConfirmLabel: string
  Pending: boolean
  OnConfirm: () => void
  OnCancel: () => void
}) {
  useEffect(() => {
    function HandleKey(Event: KeyboardEvent) {
      if (Event.key === 'Escape') OnCancel()
    }
    window.addEventListener('keydown', HandleKey)
    return () => window.removeEventListener('keydown', HandleKey)
  }, [OnCancel])

  return (
    <div className="dialog-backdrop" onMouseDown={Event => { if (Event.target === Event.currentTarget) OnCancel() }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title">
        <h3 id="dialog-title">{Title}</h3>
        <p>{Description}</p>
        <div className="dialog-actions">
          <button className="secondary" type="button" onClick={OnCancel}>Cancel</button>
          <button className="danger" type="button" onClick={OnConfirm} disabled={Pending}>
            {Pending ? 'Working…' : ConfirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
