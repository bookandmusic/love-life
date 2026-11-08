import { login as loveLifeLogin, logout as loveLifeLogout } from '@/api/auth';
import { getRole } from '@/lib/http';

/**
 * 认证适配层：模板 auth store 的入口签名不变，内部调用本仓的登录接口
 * （POST /api/v1/auth/login，登录时即完成 admin 角色校验，非 admin 直接拒绝）。
 * token/refresh 存取统一在 src/lib/http.ts 的 storage keys，不引入第二套存储。
 */

/** 与模板 request 客户端同构的扁平响应（可判别联合：error 为 null 时 data 必非空） */
type FlatOk<T> = { data: T; error: null };
type FlatFail = { data: null; error: Error };

/**
 * Login
 *
 * @param userName User name
 * @param password Password
 */
export async function fetchLogin(userName: string, password: string): Promise<FlatOk<Api.Auth.LoginToken> | FlatFail> {
  try {
    const res = await loveLifeLogin(userName, password);
    const data: Api.Auth.LoginToken = {
      token: res.token,
      refreshToken: res.refresh_token
    };
    return { data, error: null };
  } catch (error) {
    return { data: null, error: error instanceof Error ? error : new Error(String(error)) };
  }
}

/** Get user info（本仓无独立 /me 接口：角色在登录响应中给出并存于本地） */
export async function fetchGetUserInfo(): Promise<FlatOk<Api.Auth.UserInfo> | FlatFail> {
  const role = getRole() || 'admin';
  const data: Api.Auth.UserInfo = {
    userId: 'admin',
    userName: 'admin',
    roles: [role],
    buttons: []
  };
  return { data, error: null };
}

/**
 * 退出登录：先吊销服务端 refresh token（失败不阻断，本地必清），再由 store 清本地态。
 */
export async function fetchLogout(): Promise<FlatOk<boolean> | FlatFail> {
  try {
    await loveLifeLogout();
    return { data: true, error: null };
  } catch (error) {
    return { data: null, error: error instanceof Error ? error : new Error(String(error)) };
  }
}
