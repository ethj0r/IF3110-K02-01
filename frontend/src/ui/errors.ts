import { ApiError } from '../api/client'

export function ErrorMessage(Error: unknown, Fallback = 'Unable to complete the request.') {
  return Error instanceof ApiError ? Error.message : Fallback
}
