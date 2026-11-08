package com.bookandmusic.lovelife.appnative.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class UserInfo(
    val id: Long,
    val username: String,
    val role: String,
    val nickname: String,
    val ai_style: String? = null,
)

@Serializable
data class LoginResponse(val token: String, val refresh_token: String, val user: UserInfo)

@Serializable
data class RefreshResponse(val token: String, val refresh_token: String)

@Serializable
data class LoginRequest(val username: String, val password: String, val client: String)

@Serializable
data class ErrorDto(val error: String? = null)

@Serializable
data class RefreshRequest(val refresh_token: String)

/** PATCH /api/v1/me：只改输出风格（昵称 / 生日由后台维护，APP 不发） */
@Serializable
data class AiStyleRequest(val ai_style: String)


/**
 * 统一选路（GET /api/v1/channels）：后端已知的全部通道 + 各自权重。
 * APP 合并内置地址后按 weight 降序逐个探活，取第一个通的。
 * cookie/expires_at 只随绿联通道出现（中转自己的会话凭据）。
 */
@Serializable
data class ChannelItemDto(
    val kind: String = "",
    val url: String? = null,
    val weight: Int = 0,
    val cookie: String? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
)

@Serializable
data class ChannelsDto(
    val items: List<ChannelItemDto> = emptyList(),
    val builtin_weight: Int? = null,
)
