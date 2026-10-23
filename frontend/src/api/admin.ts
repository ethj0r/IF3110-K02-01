import { ApiRequest } from './client'

export type AccessLevel = 'view_only' | 'view_edit'

export interface AdminGroupRef {
  id: string
  name: string
}

export interface AdminUser {
  id: string
  username: string
  is_platform_admin: boolean
  is_active: boolean
  created_at: string
  groups: AdminGroupRef[]
}

export interface AdminGroup {
  id: string
  name: string
  description: string
  member_count: number
  access_count: number
  created_at: string
  updated_at: string
}

export interface AdminMember {
  id: string
  username: string
  is_platform_admin: boolean
  is_active: boolean
}

export interface AdminGroupAccess {
  database_id: string
  database_name: string
  host: string
  dbname: string
  level: AccessLevel
}

export interface AdminDatabaseRef {
  id: string
  name: string
  host: string
  dbname: string
}

export interface AdminGrant {
  group_id: string
  database_id: string
  level: AccessLevel
}

export interface AdminMatrix {
  groups: AdminGroup[]
  databases: AdminDatabaseRef[]
  grants: AdminGrant[]
}

export const AdminQueryKeys = {
  users: ['admin', 'users'] as const,
  groups: ['admin', 'groups'] as const,
  members: (GroupID: string) => ['admin', 'groups', GroupID, 'members'] as const,
  matrix: ['admin', 'access'] as const,
}

function PathID(ID: string) {
  return encodeURIComponent(ID)
}

export function ListUsers() {
  return ApiRequest<AdminUser[]>('/api/admin/users')
}

export function CreateUser(Input: { username: string; password: string }) {
  return ApiRequest<AdminUser>('/api/admin/users', { method: 'POST', body: JSON.stringify(Input) })
}

export function SetUserActive(ID: string, IsActive: boolean) {
  return ApiRequest<AdminUser>(`/api/admin/users/${PathID(ID)}`, { method: 'PATCH', body: JSON.stringify({ is_active: IsActive }) })
}

export function ResetUserPassword(ID: string, Password: string) {
  return ApiRequest<AdminUser>(`/api/admin/users/${PathID(ID)}/password`, { method: 'PUT', body: JSON.stringify({ password: Password }) })
}

export function DeleteUser(ID: string) {
  return ApiRequest<void>(`/api/admin/users/${PathID(ID)}`, { method: 'DELETE' })
}

export function ListGroups() {
  return ApiRequest<AdminGroup[]>('/api/admin/groups')
}

export function CreateGroup(Input: { name: string; description: string }) {
  return ApiRequest<AdminGroup>('/api/admin/groups', { method: 'POST', body: JSON.stringify(Input) })
}

export function UpdateGroup(ID: string, Input: { name?: string; description?: string }) {
  return ApiRequest<AdminGroup>(`/api/admin/groups/${PathID(ID)}`, { method: 'PATCH', body: JSON.stringify(Input) })
}

export function DeleteGroup(ID: string) {
  return ApiRequest<void>(`/api/admin/groups/${PathID(ID)}`, { method: 'DELETE' })
}

export function ListMembers(GroupID: string) {
  return ApiRequest<AdminMember[]>(`/api/admin/groups/${PathID(GroupID)}/members`)
}

export function AddMember(GroupID: string, UserID: string) {
  return ApiRequest<void>(`/api/admin/groups/${PathID(GroupID)}/members/${PathID(UserID)}`, { method: 'PUT' })
}

export function RemoveMember(GroupID: string, UserID: string) {
  return ApiRequest<void>(`/api/admin/groups/${PathID(GroupID)}/members/${PathID(UserID)}`, { method: 'DELETE' })
}

export function ListGroupAccess(GroupID: string) {
  return ApiRequest<AdminGroupAccess[]>(`/api/admin/groups/${PathID(GroupID)}/access`)
}

export function SetGroupAccess(GroupID: string, DatabaseID: string, Level: AccessLevel) {
  return ApiRequest<void>(`/api/admin/groups/${PathID(GroupID)}/access/${PathID(DatabaseID)}`, { method: 'PUT', body: JSON.stringify({ level: Level }) })
}

export function RemoveGroupAccess(GroupID: string, DatabaseID: string) {
  return ApiRequest<void>(`/api/admin/groups/${PathID(GroupID)}/access/${PathID(DatabaseID)}`, { method: 'DELETE' })
}

export function GetMatrix() {
  return ApiRequest<AdminMatrix>('/api/admin/access')
}
