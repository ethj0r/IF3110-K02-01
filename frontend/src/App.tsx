import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthGuard } from './auth/Guard'
import { LoginPage } from './pages/Login'
import { MigrationsPage } from './pages/Migrations'
import { Workspace } from './pages/Workspace'

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthGuard />}>
        <Route path="/" element={<Workspace />} />
        <Route path="/databases/:dbId/migrations" element={<MigrationsPage />} />
        <Route path="/migrations" element={<MigrationsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
