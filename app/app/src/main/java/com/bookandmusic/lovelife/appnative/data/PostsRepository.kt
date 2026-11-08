package com.bookandmusic.lovelife.appnative.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class MediaDto(
    val id: Long,
    val kind: String? = null,
    val thumb_url: String? = null,
    val url: String? = null,
    val duration: Double? = null,
    val width: Int = 0,
    val height: Int = 0,
    val author_id: Long = 0,
)

@Serializable
data class PostDto(
    val id: Long,
    val author_id: Long = 0,
    val author: String,
    val author_avatar: String? = null,
    val content: String? = null,
    val mood: String? = null,
    val location_name: String? = null,
    val created_at: String,
    val media: List<MediaDto> = emptyList(),
    val like_count: Int = 0,
    val liked: Boolean = false,
    val likers: List<LikerDto> = emptyList(),
    val comment_count: Int = 0,
)

/** 点赞人（只有两名用户，服务端直接全量下发）。 */
@Serializable
data class LikerDto(
    val id: Long,
    val nickname: String,
)

/** 列表结果（服务端 total 用于「没有更多了」判定）。 */
data class PostsResult(
    val items: List<PostUi>,
    val total: Int,
)

@Serializable
data class PostsPage(
    val items: List<PostDto> = emptyList(),
    val total: Int = 0,
    val page: Int = 1,
    val page_size: Int = 20,
)

@Serializable
data class ReurlRequest(val ids: List<Long>)

@Serializable
data class ReurlItem(val id: Long, val thumb_url: String? = null, val url: String? = null)

@Serializable
data class ReurlResponse(val items: List<ReurlItem> = emptyList())

@Serializable
data class LikeResponse(val liked: Boolean = false)

/** 地点对象：地图选点/候选回填带坐标；纯文本地名走 location_name（服务端 location 优先） */
@Serializable
data class PostLocation(
    val name: String,
    val lat: Double? = null,
    val lng: Double? = null,
)

@Serializable
data class CreatePostRequest(
    val content: String = "",
    val mood: String = "",
    val location_name: String = "",
    val location: PostLocation? = null,
    val media_tokens: List<String> = emptyList(),
)

@Serializable
data class CreatedId(val id: Long)

/** 删除单条媒体的结果：post_deleted=true 表示所属动态已被一并删除 */
@Serializable
data class MediaDeleteResponse(
    @SerialName("post_deleted") val postDeleted: Boolean = false,
)

/** 单条媒体（kind: photo/video/audio；地址已换签拼 base，可直接加载/播放）。 */
data class PostMediaUi(
    /** 媒体行 id（单独删除用；相册视图的 id 语义同 media 表主键） */
    val id: Long = 0,
    val kind: String?,
    val thumbUrl: String?,
    val url: String?,
    val durationText: String? = null,
    /** 原始宽高（单图按真实比例显示） */
    val width: Int = 0,
    val height: Int = 0,
    /** 媒体作者 id（0 = 未知）：长按菜单「删除」项仅作者可见（保存/移出相册不受限） */
    val authorId: Long = 0,
)

/** 一条动态的展示模型。 */
data class PostUi(
    val id: Long,
    val authorId: Long,
    val author: String,
    /** 作者头像完整 URL（空串=无头像，回退首字色块） */
    val authorAvatar: String = "",
    val content: String,
    val mood: String?,
    val locationName: String?,
    val createdAt: String,
    val likeCount: Int,
    val liked: Boolean,
    /** 点赞人昵称（「张三、李四 赞了」用） */
    val likers: List<String>,
    val commentCount: Int,
    val media: List<PostMediaUi>,
)

/**
 * 动态流：分页拉取 + 媒体地址拼通道 base（服务端已签名，15 分钟 TTL），
 * 点赞 / 收藏 / 评论 / 软删 / 发布；回忆精选走 sample=1，那年今日走 memories=1（服务端同月同日过滤）。
 */
object PostsRepository {

    suspend fun posts(
        page: Int = 1,
        pageSize: Int = 20,
        memories: Boolean = false,
        sample: Boolean = false,
        hasMedia: Boolean? = null,
    ): PostsResult {
        val query = buildString {
            append("/api/v1/posts?page=$page&page_size=$pageSize")
            if (memories) append("&memories=1")
            if (sample) append("&sample=1")
            if (hasMedia != null) append(if (hasMedia) "&media=1" else "&media=0")
        }
        val res = ApiClient.api<PostsPage>(query)
        return PostsResult(items = res.items.map { it.toUi() }, total = res.total)
    }

    /** 回忆精选：全量池随机抽（有图 / 无图两层各自抽，保证封面卡那层非空）。 */
    suspend fun sampled(hasMedia: Boolean, pageSize: Int = 20): List<PostUi> =
        posts(page = 1, pageSize = pageSize, sample = true, hasMedia = hasMedia).items

    /** 那年今日：同月同日的历史动态（服务端过滤后再分页）。 */
    suspend fun memories(pageSize: Int = 50): List<PostUi> =
        posts(page = 1, pageSize = pageSize, memories = true).items

    suspend fun toggleLike(postId: Long): Boolean =
        ApiClient.api<LikeResponse>("/api/v1/posts/$postId/like", "POST", "{}").liked

    /** 发布动态（媒体先上传拿 token，再一次性提交） */
    suspend fun create(
        content: String,
        mood: String,
        locationName: String,
        mediaTokens: List<String>,
        location: PostLocation? = null,
    ): Long {
        val body = ApiClient.json.encodeToString(
            CreatePostRequest.serializer(),
            CreatePostRequest(content, mood, locationName, location, mediaTokens),
        )
        return ApiClient.api<CreatedId>("/api/v1/posts", "POST", body).id
    }

    /** 软删动态（仅作者；媒体一并进回收站） */
    suspend fun delete(postId: Long) {
        ApiClient.api<Unit>("/api/v1/posts/$postId", "DELETE")
    }

    /** 单独删除一条媒体（仅作者）；返回 true = 所属动态也因无媒体且无正文而一并被删 */
    suspend fun deleteMedia(mediaId: Long): Boolean =
        ApiClient.api<MediaDeleteResponse>("/api/v1/media/$mediaId", "DELETE").postDeleted

    /**
     * 媒体签名 URL 批量重签（签名 15 分钟 TTL）：播放/另存前对临期地址换新。
     * 返回「id → 换新后的 raw 相对路径」；失败由调用方兜底（用原地址试一次）。
     */
    suspend fun reurl(mediaIds: List<Long>): Map<Long, String> {
        if (mediaIds.isEmpty()) return emptyMap()
        val body = ApiClient.json.encodeToString(ReurlRequest.serializer(), ReurlRequest(mediaIds))
        val res = ApiClient.api<ReurlResponse>("/api/v1/media/reurl", "POST", body)
        return res.items.mapNotNull { it.url?.let { u -> it.id to u } }.toMap()
    }

    private fun PostDto.toUi(): PostUi {
        val base = Channel.base().trimEnd('/')
        return PostUi(
            id = id,
            authorId = author_id,
            author = author,
            // 已签名 URL（含 exp/sig），拼 base 即可直接加载；空串 = 无头像
            authorAvatar = author_avatar?.takeIf { it.isNotEmpty() }?.let { base + it } ?: "",
            content = content ?: "",
            mood = mood?.takeIf { it.isNotEmpty() },
            locationName = location_name?.takeIf { it.isNotEmpty() },
            createdAt = created_at,
            likeCount = like_count,
            liked = liked,
            likers = likers.map { it.nickname },
            commentCount = comment_count,
            media = media.map { m ->
                PostMediaUi(
                    id = m.id,
                    kind = m.kind,
                    thumbUrl = m.thumb_url?.let { base + it },
                    url = m.url?.let { base + it },
                    durationText = m.duration?.let { fmtDuration(it) },
                    width = m.width,
                    height = m.height,
                    authorId = author_id,
                )
            },
        )
    }

    private fun fmtDuration(seconds: Double): String {
        val total = seconds.toInt()
        val m = total / 60
        val s = total % 60
        return if (m > 0) "$m:${s.toString().padStart(2, '0')}" else "0:${s.toString().padStart(2, '0')}"
    }
}
