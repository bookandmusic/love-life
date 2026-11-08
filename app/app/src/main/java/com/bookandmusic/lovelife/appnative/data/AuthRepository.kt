package com.bookandmusic.lovelife.appnative.data


/** 会话与凭据：登录 / 记住登录 / 启动恢复三分支。 */
object AuthRepository {

    sealed interface RestoreResult {
        data object Ok : RestoreResult
        data object Expired : RestoreResult
        data object Network : RestoreResult
    }

    suspend fun login(username: String, password: String): UserInfo {
        val body = ApiClient.json.encodeToString(
            LoginRequest.serializer(),
            LoginRequest(username, password, "app"),
        )
        val res = ApiClient.api<LoginResponse>("/api/v1/auth/login", "POST", body, retry = false)
        SecureStore.setTokens(res.token, res.refresh_token)
        return res.user
    }

    /** 记住登录：凭据写入安全存储；失败不阻断登录（免登录仅靠 refresh token） */
    fun rememberCredentials(username: String, password: String): Boolean =
        SecureStore.saveCredentials(username, password)

    /** 登录页预填：读取已存凭据；无则 null（不自动提交） */
    fun loadSavedCredentials(): Pair<String, String>? = SecureStore.loadCredentials()

    /** 取消「记住登录」或退出登录时清除凭据 */
    fun forgetCredentials() = SecureStore.clearCredentials()

    /** 改输出风格（个人偏好，跨设备生效）：服务端校验风格名是否在风格库中，不在库中返回 400 */
    suspend fun updateAiStyle(name: String) {
        val body = ApiClient.json.encodeToString(AiStyleRequest.serializer(), AiStyleRequest(name))
        ApiClient.api<Unit>("/api/v1/me", "PATCH", body)
    }

    suspend fun restore(): UserInfo? =
        if (SecureStore.token() == null) {
            null
        } else {
            try {
                ApiClient.api<UserInfo>("/api/v1/auth/me")
            } catch (_: Exception) {
                null
            }
        }

    /**
     * 启动恢复（Boot 用）：区分「凭据失效」与「网络失败」。
     * 401 且 refresh 已失败 → 有已存凭据则静默重登一次，仍失败判失效；
     * 非 401（网络/服务端异常）→ 不清凭据，判网络让用户重试。
     */
    suspend fun restoreWithRetry(): RestoreResult {
        if (SecureStore.token() == null) return RestoreResult.Expired
        val me = try {
            ApiClient.api<UserInfo>("/api/v1/auth/me")
        } catch (e: Exception) {
            if ((e as? ApiError)?.status != 401) return RestoreResult.Network
            val saved = SecureStore.loadCredentials() ?: return RestoreResult.Expired
            try {
                login(saved.first, saved.second)
                return RestoreResult.Ok
            } catch (e2: Exception) {
                return if ((e2 as? ApiError)?.status == 401) RestoreResult.Expired else RestoreResult.Network
            }
        }
        return RestoreResult.Ok
    }
}
