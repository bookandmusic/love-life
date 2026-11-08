import { clearAuth, getToken as getTokenFromLib } from '@/lib/http';

/** Get token（存取统一走 src/lib/http 的 storage keys，与业务请求层同一来源） */
export function getToken() {
  return getTokenFromLib() || '';
}

/** Clear auth storage */
export function clearAuthStorage() {
  clearAuth();
}
