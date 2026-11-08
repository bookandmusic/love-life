package com.bookandmusic.lovelife.appnative.data

import kotlinx.serialization.Serializable

@Serializable
data class SpaceMemberDto(
    val id: Long = 0,
    val nickname: String,
    val role: String,
    val avatar: String? = null,
)

@Serializable
data class NextAnniversaryDto(
    val name: String = "",
    val date: String = "",
    val days_until: Int = 0,
)

@Serializable
data class SpaceInfoDto(
    val name: String = "",
    val signature: String = "",
    val love_start_date: String? = null,
    val members: List<SpaceMemberDto> = emptyList(),
    val days_together: Long? = null,
    val next_anniversary: NextAnniversaryDto? = null,
    val anniversary_count: Int = 0,
    val album_count: Int = 0,
)

object SpaceRepository {
    suspend fun info(): SpaceInfoDto = ApiClient.api("/api/v1/space")
}

@Serializable
data class AnniversaryDto(
    val id: Long,
    val name: String,
    val date: String,
    val type: String? = null,
    val is_leap: Boolean = false,
    val remind_days_before: Int = 0,
    val days_until: Int = 0,
    val created_by: Long = 0,
)

@Serializable
data class AnniversaryListDto(val items: List<AnniversaryDto> = emptyList())

@Serializable
data class CreateAnniversaryRequest(
    val name: String,
    val date: String,
    val type: String = "solar",
    val is_leap: Boolean = false,
    val remind_days_before: Int = 1,
)

object AnniversariesRepository {
    /** 距离当前最近（days_until 升序）优先，预览与列表页共用此口径 */
    suspend fun list(): List<AnniversaryDto> =
        ApiClient.api<AnniversaryListDto>("/api/v1/anniversaries").items.sortedBy { it.days_until }

    suspend fun create(name: String, date: String, type: String, isLeap: Boolean, remindDays: Int): Long {
        val body = ApiClient.json.encodeToString(
            CreateAnniversaryRequest.serializer(),
            CreateAnniversaryRequest(name, date, type, isLeap, remindDays),
        )
        return ApiClient.api<CreatedId>("/api/v1/anniversaries", "POST", body).id
    }

    /** 软删（204，进回收站，仅 admin 后台可恢复） */
    suspend fun delete(id: Long) {
        ApiClient.api<Unit>("/api/v1/anniversaries/$id", "DELETE")
    }
}

@Serializable
data class AlbumDto(
    val id: Long,
    val name: String,
    val description: String = "",
    val item_count: Int = 0,
    val cover_url: String? = null,
    val cover_width: Int = 0,
    val cover_height: Int = 0,
    val created_by: Long = 0,
)

@Serializable
data class AlbumListDto(val items: List<AlbumDto> = emptyList())

@Serializable
data class TimelineItemDto(
    val id: Long,
    val kind: String? = null,
    val thumb_url: String? = null,
    val url: String? = null,
    val taken_at: String? = null,
    val month_key: String? = null,
    val duration: Double? = null,
    val post_id: Long = 0,
    /** 媒体作者 id（服务端 mediaItem 已下发）：APP 据此判定「删除」项是否出现在长按菜单 */
    val author_id: Long = 0,
    val album_id: Long? = null,
    val album_name: String? = null,
)

@Serializable
data class TimelinePageDto(
    val items: List<TimelineItemDto> = emptyList(),
    val total: Int = 0,
    val has_more: Boolean = false,
)

@Serializable
data class AlbumDetailDto(
    val album: AlbumNameDto? = null,
    val media: List<TimelineItemDto> = emptyList(),
)

@Serializable
data class AlbumNameDto(val name: String)

@Serializable
data class CreateAlbumRequest(val name: String, val description: String = "")

@Serializable
data class AlbumPhotosRequest(val media_ids: List<Long>)

object AlbumsRepository {
    suspend fun albums(): List<AlbumDto> =
        ApiClient.api<AlbumListDto>("/api/v1/albums").items

    /** 服务端单页硬上限（更大值会被夹到 50） */
    const val TIMELINE_PAGE_SIZE = 50

    /** 时间轴 = 未归档媒体（服务端默认口径；scope=all 才含已归档） */
    suspend fun timeline(page: Int = 1, size: Int = TIMELINE_PAGE_SIZE): TimelinePageDto =
        ApiClient.api("/api/v1/media/timeline?page=$page&size=$size")

    /** 拉全时间轴（选图器用）：按 has_more 翻页，最多 [maxPages] 页兜底防失控 */
    suspend fun timelineAll(maxPages: Int = 20): List<TimelineItemDto> {
        val out = mutableListOf<TimelineItemDto>()
        var page = 1
        while (page <= maxPages) {
            val res = timeline(page)
            out += res.items
            if (!res.has_more || res.items.isEmpty()) break
            page++
        }
        return out
    }

    suspend fun albumMedia(albumId: Long): AlbumDetailDto =
        ApiClient.api("/api/v1/albums/$albumId")

    suspend fun create(name: String, description: String = ""): Long {
        val body = ApiClient.json.encodeToString(CreateAlbumRequest.serializer(), CreateAlbumRequest(name, description))
        return ApiClient.api<CreatedId>("/api/v1/albums", "POST", body).id
    }

    suspend fun addPhotos(albumId: Long, mediaIds: List<Long>) {
        val body = ApiClient.json.encodeToString(AlbumPhotosRequest.serializer(), AlbumPhotosRequest(mediaIds))
        ApiClient.api<Unit>("/api/v1/albums/$albumId/photos", "POST", body)
    }

    suspend fun removePhotos(albumId: Long, mediaIds: List<Long>) {
        val body = ApiClient.json.encodeToString(AlbumPhotosRequest.serializer(), AlbumPhotosRequest(mediaIds))
        ApiClient.api<Unit>("/api/v1/albums/$albumId/photos", "DELETE", body)
    }

    /** 软删相册（媒体回时间轴，不删媒体） */
    suspend fun delete(albumId: Long) {
        ApiClient.api<Unit>("/api/v1/albums/$albumId", "DELETE")
    }
}

@Serializable
data class PointDto(
    val id: Long,
    val name: String,
    val lat: Double,
    val lng: Double,
    val visit_date: String? = null,
    val description: String? = null,
    val source: String? = null,
    val created_at: String? = null,
    val created_by: Long = 0,
)

@Serializable
data class PointListDto(val items: List<PointDto> = emptyList())

@Serializable
data class CreatePointRequest(
    val name: String,
    val lat: Double,
    val lng: Double,
    val visit_date: String = "",
    val description: String = "",
)

@Serializable
data class GeocodeCandidate(
    val name: String = "",
    val city: String = "",
    val lat: Double = 0.0,
    val lng: Double = 0.0,
    val display: String = "",
)

@Serializable
data class GeocodeResponse(val candidates: List<GeocodeCandidate> = emptyList())

object PointsRepository {
    /** 时间降序（visit_date 优先，空则 created_at；无日期的排最后） */
    suspend fun list(): List<PointDto> =
        ApiClient.api<PointListDto>("/api/v1/map/points").items
            .sortedByDescending { it.visit_date ?: it.created_at ?: "" }

    suspend fun create(name: String, lat: Double, lng: Double, visitDate: String = "", description: String = ""): Long {
        val body = ApiClient.json.encodeToString(
            CreatePointRequest.serializer(),
            CreatePointRequest(name, lat, lng, visitDate, description),
        )
        return ApiClient.api<CreatedId>("/api/v1/map/points", "POST", body).id
    }

    suspend fun geocode(q: String): List<GeocodeCandidate> =
        ApiClient.api<GeocodeResponse>("/api/v1/geocode?q=${java.net.URLEncoder.encode(q, "UTF-8")}").candidates

    /** 软删点位（动态派生点位删除不删动态） */
    suspend fun delete(id: Long) {
        ApiClient.api<Unit>("/api/v1/map/points/$id", "DELETE")
    }
}

@Serializable
data class CommentDto(
    val id: Long,
    val author: String,
    val author_id: Long = 0,
    val content: String,
    val created_at: String = "",
)

@Serializable
data class CommentListDto(val items: List<CommentDto> = emptyList())

@Serializable
data class CreateCommentRequest(val content: String)

object CommentsRepository {
    /** 删除本人评论（仅作者） */
    suspend fun delete(id: Long) {
        ApiClient.api<Unit>("/api/v1/comments/$id", "DELETE")
    }

    suspend fun create(postId: Long, content: String): CommentDto {
        val body = ApiClient.json.encodeToString(CreateCommentRequest.serializer(), CreateCommentRequest(content))
        return ApiClient.api("/api/v1/posts/$postId/comments", "POST", body)
    }

    /** 评论列表（响应可能是 {items:[…]} 或裸数组，两种都容错） */
    suspend fun list(postId: Long): List<CommentDto> {
        val text = ApiClient.apiRaw("/api/v1/posts/$postId/comments", "GET", null, retry = false) ?: return emptyList()
        return runCatching {
            ApiClient.json.decodeFromString(CommentListDto.serializer(), text).items
        }.getOrElse {
            runCatching {
                ApiClient.json.decodeFromString(kotlinx.serialization.builtins.ListSerializer(CommentDto.serializer()), text)
            }.getOrDefault(emptyList())
        }
    }
}

@Serializable
data class NotificationDto(
    val id: Long,
    val event_type: String = "",
    val target_type: String = "",
    val target_id: Long = 0,
    val snapshot: String = "",
    val created_at: String = "",
)

@Serializable
data class NotificationListDto(val items: List<NotificationDto> = emptyList())

@Serializable
data class UnreadCountDto(val count: Int = 0)

/** 通知中心：列表 / 未读数 / 点击即删 / 清空（通知：临时消费品，无开关）。 */
object NotificationsRepository {

    /** 服务端固定每页 50 条（只收 page，不收 size） */
    private const val PAGE_SIZE = 50

    suspend fun list(page: Int = 1): List<NotificationDto> =
        ApiClient.api<NotificationListDto>("/api/v1/notifications?page=$page").items

    /** 拉全通知：取到不满一页为止，最多 [maxPages] 页兜底防失控 */
    suspend fun listAll(maxPages: Int = 20): List<NotificationDto> {
        val out = mutableListOf<NotificationDto>()
        for (page in 1..maxPages) {
            val items = list(page)
            out += items
            if (items.size < PAGE_SIZE) break
        }
        return out
    }

    suspend fun unreadCount(): Int =
        runCatching { ApiClient.api<UnreadCountDto>("/api/v1/notifications/unread-count").count }.getOrDefault(0)

    suspend fun consume(id: Long) {
        ApiClient.api<Unit>("/api/v1/notifications/$id", "DELETE")
    }

    suspend fun clear() {
        ApiClient.api<Unit>("/api/v1/notifications", "DELETE")
    }
}

@Serializable
private data class StyleItemDto(val name: String = "")

@Serializable
private data class StylesDto(val items: List<StyleItemDto> = emptyList())

/** 输出风格库（服务端维护）：APP 只取名字做选择项，提示词在后台配置 */
object StylesRepository {
    suspend fun names(): List<String> =
        ApiClient.api<StylesDto>("/api/v1/styles").items.map { it.name }.filter { it.isNotBlank() }
}
