import { Navigate, Route, Routes } from 'react-router-dom'
import { AdminGuard } from './auth/AdminGuard'
import { AuthGuard } from './auth/Guard'
import { AppShell } from './layout/AppShell'
import { DatabaseDetailPage } from './pages/DatabaseDetail'
import { LoginPage } from './pages/Login'
import { MigrationsPage } from './pages/Migrations'
import { SchemaDiffPage } from './pages/SchemaDiff'
import { Workspace } from './pages/Workspace'
import { AccessMatrixPage } from './pages/admin/AccessMatrix'
import { ConnectionsPage } from './pages/admin/Connections'
import { GroupsPage } from './pages/admin/Groups'
import { UsersPage } from './pages/admin/Users'

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthGuard />}>
        <Route element={<AppShell />}>
          <Route path="/" element={<Workspace />} />
          <Route path="/migrations" element={<MigrationsPage />} />
          <Route path="/databases/:id" element={<DatabaseDetailPage />} />
          <Route path="/databases/:dbId/migrations" element={<MigrationsPage />} />
          <Route path="/databases/:dbId/schema-diff" element={<SchemaDiffPage />} />
          <Route path="/admin" element={<AdminGuard />}>
            <Route index element={<Navigate to="users" replace />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="groups" element={<GroupsPage />} />
            <Route path="access" element={<AccessMatrixPage />} />
            <Route path="connections" element={<ConnectionsPage />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  )
}
