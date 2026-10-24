import { Navigate, Route, Routes } from 'react-router-dom'
import { AdminGuard } from './auth/AdminGuard'
import { AuthGuard } from './auth/Guard'
import { LoginPage } from './pages/Login'
import { MigrationsPage } from './pages/Migrations'
import { Workspace } from './pages/Workspace'
import { DatabaseDetailPage } from './pages/DatabaseDetail'
import { AccessMatrixPage } from './pages/admin/AccessMatrix'
import { AdminLayout } from './pages/admin/AdminLayout'
import { ConnectionsPage } from './pages/admin/Connections'
import { GroupsPage } from './pages/admin/Groups'
import { UsersPage } from './pages/admin/Users'

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthGuard />}>
        <Route path="/" element={<Workspace />} />
        <Route path="/databases/:dbId/migrations" element={<MigrationsPage />} />
        <Route path="/migrations" element={<MigrationsPage />} />
        <Route path="/databases/:id" element={<DatabaseDetailPage />} />
        <Route path="/admin" element={<AdminGuard />}>
          <Route element={<AdminLayout />}>
            <Route index element={<Navigate to="users" replace />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="groups" element={<GroupsPage />} />
            <Route path="access" element={<AccessMatrixPage />} />
            <Route path="connections" element={<ConnectionsPage />} />
          </Route>
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}