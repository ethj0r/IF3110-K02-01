import type { ReactNode } from 'react'

export type IconName =
  | 'database'
  | 'layers'
  | 'users'
  | 'group'
  | 'shield'
  | 'plug'
  | 'menu'
  | 'close'
  | 'plus'
  | 'chevron-left'
  | 'chevron-right'
  | 'arrow-right'
  | 'logout'
  | 'check'
  | 'alert'
  | 'info'
  | 'diff'
  | 'table'
  | 'history'
  | 'play'
  | 'search'
  | 'lock'

const Paths: Record<IconName, ReactNode> = {
  database: (
    <>
      <ellipse cx="12" cy="5.5" rx="7" ry="2.5" />
      <path d="M5 5.5v6.5c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5V5.5" />
      <path d="M5 12v6.5c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5V12" />
    </>
  ),
  layers: (
    <>
      <path d="m12 3 9 4.5-9 4.5-9-4.5L12 3Z" />
      <path d="m3 12 9 4.5 9-4.5" />
      <path d="m3 16.5 9 4.5 9-4.5" />
    </>
  ),
  users: (
    <>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M5 20c.8-3.4 3.6-5.5 7-5.5s6.2 2.1 7 5.5" />
    </>
  ),
  group: (
    <>
      <circle cx="9" cy="8.5" r="3" />
      <path d="M3.5 19c.6-2.9 2.8-4.7 5.5-4.7s4.9 1.8 5.5 4.7" />
      <path d="M15.5 5.8a3 3 0 0 1 0 5.4" />
      <path d="M17 14.6c1.8.6 3.1 2.1 3.5 4.4" />
    </>
  ),
  shield: (
    <>
      <path d="M12 3 5 5.8v5.6c0 4.3 2.9 7.8 7 9.1 4.1-1.3 7-4.8 7-9.1V5.8L12 3Z" />
      <path d="m9.2 12 2 2 3.7-3.9" />
    </>
  ),
  plug: (
    <>
      <path d="M9 3v4M15 3v4" />
      <path d="M6.5 7h11v3.5a5.5 5.5 0 0 1-11 0V7Z" />
      <path d="M12 16v5" />
    </>
  ),
  menu: <path d="M4 7h16M4 12h16M4 17h16" />,
  close: <path d="m6 6 12 12M18 6 6 18" />,
  plus: <path d="M12 5v14M5 12h14" />,
  'chevron-left': <path d="m14.5 6-6 6 6 6" />,
  'chevron-right': <path d="m9.5 6 6 6-6 6" />,
  'arrow-right': <path d="M5 12h14m-5-5 5 5-5 5" />,
  logout: (
    <>
      <path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4" />
      <path d="m10 16-4-4 4-4M6 12h9" />
    </>
  ),
  check: <path d="m5 12.5 4.5 4.5L19 7.5" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7.5v5.5M12 16.4v.1" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5.5M12 7.6v.1" />
    </>
  ),
  diff: (
    <>
      <path d="M7 4v11M3.5 8 7 4l3.5 4" />
      <path d="M17 20V9m3.5 7L17 20l-3.5-4" />
    </>
  ),
  table: (
    <>
      <rect x="3.5" y="4.5" width="17" height="15" rx="2.5" />
      <path d="M3.5 9.5h17M9.5 9.5v10" />
    </>
  ),
  history: (
    <>
      <path d="M4 12a8 8 0 1 0 2.4-5.7L4 8.5" />
      <path d="M4 4v4.5h4.5M12 8v4.5l3 2" />
    </>
  ),
  play: <path d="M8 5.5v13l10-6.5-10-6.5Z" />,
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m20 20-4.4-4.4" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="9.5" rx="2.5" />
      <path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5" />
    </>
  ),
}

export function Icon({ Name, Size = 16 }: { Name: IconName; Size?: number }) {
  return (
    <svg
      width={Size}
      height={Size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {Paths[Name]}
    </svg>
  )
}
