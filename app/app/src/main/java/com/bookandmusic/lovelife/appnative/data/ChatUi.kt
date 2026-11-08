package com.bookandmusic.lovelife.appnative.data

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * 卡片体系的数据层（服务端 tools 契约 / 正文 ```ui 围栏 / 确认卡预览）。
 *
 * 三段分离：数据 [ResultData] → 卡片提案 [CardSpec]（模型写围栏）→ 渲染卡 [CardEvent]（服务端校验后下发）。
 * 客户端**不推断形态**：画成哪种卡只看 [CardEvent.view]——它由服务端 `validated: true` 保证。
 * 引用不存在 / 未校验 / 形态不认识 / 没有数据的围栏在 [UiFence] 就落成一行人话，不降级、不猜。
 */
object ChatUi {

    /** 七种渲染形态：服务端校验只放行这七个 */
    val VIEWS = listOf("list", "grid", "stats", "detail", "kpi", "timeline", "map")

    // ---------- 数据（工具返回，只给数据不给画法） ----------

    /** 工具结果的结构化数据；[ref] 是本轮结果集句柄，模型围栏引用它 */
    @Serializable
    data class ResultData(
        val ref: String = "",
        val kind: String = "", // records | media | entity | stats
        val title: String = "",
        val total: Int = 0,
        val records: List<UiRecord> = emptyList(),
        val entity: UiEntity? = null,
        val stats: List<UiStat> = emptyList(),
        val series: List<UiSeries> = emptyList(),
        val summaries: List<UiSummary> = emptyList(),
        val selectable: Boolean = false,
    )

    /** 详情卡的单实体载荷（一条动态 / 一个相册 / 一个纪念日 / 一个地点 / 一条评论 / 一张媒体） */
    @Serializable
    data class UiEntity(
        val kind: String = "",
        val id: Long = 0,
        val title: String = "",
        val text: String = "",
        val date: String = "",
        val place: String = "",
        val mood: String = "",
        val media: List<UiRecord> = emptyList(),
        val meta: List<UiStat> = emptyList(),
    )

    @Serializable
    data class UiRecord(
        val id: Long = 0,
        val kind: String = "",
        val text: String = "",
        val date: String = "",
        val thumb_url: String = "",
        val badge: String = "",
        /** 点位坐标（只有地点条目带；地图卡用，0 视为没有坐标） */
        val lat: Double = 0.0,
        val lng: Double = 0.0,
    )

    @Serializable
    data class UiStat(val label: String = "", val value: String = "", val hint: String = "")

    @Serializable
    data class UiSeries(val title: String = "", val points: List<UiSeriesPoint> = emptyList())

    @Serializable
    data class UiSeriesPoint(val label: String = "", val count: Int = 0)

    /** 分布摘要的一行（「按年」+「2024–2026 · 3 年共 498 条」） */
    @Serializable
    data class UiSummary(val label: String = "", val text: String = "")

    // ---------- 卡片提案（模型在正文围栏里写） ----------

    /** 模型写的卡片提案；服务端逐条校验（ref 存在 / 形态与数据匹配），客户端只取 ref 查 [CardEvent] */
    @Serializable
    data class CardSpec(
        val ref: String = "",
        val view: String = "",
        val title: String? = null,
        val note: String? = null,
        /** 只画这些项（仅 stats / kpi；按卡上的项名匹配） */
        val only: List<String> = emptyList(),
        /** 列表最多画几条（仅 list / grid；0 = 数据默认条数） */
        val limit: Int = 0,
    )

    // ---------- 渲染卡（服务端校验后下发） ----------

    /**
     * 一条已验证的渲染卡（SSE `card` 事件 / 历史 `result_card` part）。
     * [validated] 为 true 才画；[view] 是最终形态——客户端不按数据重推。
     */
    @Serializable
    data class CardEvent(
        val validated: Boolean = false,
        val view: String = "",
        val ref: String = "",
        val title: String = "",
        val note: String = "",
        /** 服务端透传的收窄指令（模型提案里写的那两项，校验后原样下发） */
        val only: List<String> = emptyList(),
        val limit: Int = 0,
        val data: ResultData? = null,
    )

    // ---------- 确认卡预览 ----------

    /** 一行「标签 → 值」 */
    data class Line(val label: String, val value: String)

    /**
     * 目标形态预览的一段：**与结果卡同一套形态词汇**（正文 / 标签行 / 变更 / 媒体 / 条目），
     * 保证「看到的就是要写的」。一张确认卡的预览是若干段的序列。
     */
    sealed interface PreviewView {
        /** 正文：要发的东西本身 */
        data class Text(val text: String) : PreviewView

        /** 元信息：心情 / 地点 / 日期 / 历法 … */
        data class Lines(val lines: List<Line>) : PreviewView

        /** 一次状态变更（点赞 / 收藏 / 相册调整等） */
        data class Change(val label: String, val from: String, val to: String) : PreviewView

        /** 媒体：缩略图（[media] 为空时只报数量） */
        data class Media(val media: List<UiRecord>, val count: Int) : PreviewView

        /** 批量条目（删除清单 / 相册增删成员） */
        data class Items(val items: List<UiRecord>, val count: Int) : PreviewView
    }

    data class CardPreview(val kind: String = "", val views: List<PreviewView> = emptyList())

    /**
     * 确认卡的预览载荷 → [CardPreview]。
     * 各意图的 body 字段不同，这里摊平成统一的 [PreviewView] 序列——渲染层不必知道 8 种 body 的形状。
     */
    fun parsePreview(p: ChatRepository.Preview?): CardPreview {
        val kind = p?.kind.orEmpty()
        val obj = p?.body as? JsonObject ?: return CardPreview(kind = kind)
        fun str(k: String) = (obj[k] as? JsonPrimitive)?.content?.takeIf { it.isNotBlank() }
        fun int(k: String) = (obj[k] as? JsonPrimitive)?.content?.toIntOrNull()
        fun rec(k: String): UiRecord? = (obj[k] as? JsonObject)?.let { parseRecord(it) }
        fun recs(k: String): List<UiRecord> =
            (obj[k] as? JsonArray)?.mapNotNull { (it as? JsonObject)?.let { o -> parseRecord(o) } }
                ?: emptyList()

        val lines = mutableListOf<Line>()
        var text = ""
        var change: PreviewView.Change? = null
        var media = emptyList<UiRecord>()
        var mediaCount = 0
        var items = emptyList<UiRecord>()
        var itemCount = 0

        when (kind) {
            "post" -> {
                text = str("content").orEmpty()
                str("mood")?.let { lines.add(Line("心情", it)) }
                str("place")?.let { pl ->
                    val city = str("place_city")
                    lines.add(Line("地点", if (city != null && city != pl) "$pl · $city" else pl))
                }
                mediaCount = recs("attachments").size
            }
            "comment" -> {
                text = str("content").orEmpty()
                rec("post")?.let { lines.add(Line("动态", it.text.ifEmpty { "#${it.id}" })) }
            }
            "album" -> {
                text = str("name").orEmpty()
                str("description")?.let { lines.add(Line("描述", it)) }
                int("count")?.let { lines.add(Line("收录", "$it 张")) }
                media = recs("media")
            }
            "album_members" -> {
                rec("album")?.let { text = it.text.ifEmpty { "相册 #${it.id}" } }
                int("add_count")?.takeIf { it > 0 }?.let { lines.add(Line("加入", "$it 个")) }
                int("remove_count")?.takeIf { it > 0 }?.let { lines.add(Line("移出", "$it 个")) }
                items = recs("add") + recs("remove")
                itemCount = items.size
            }
            "anniversary" -> {
                text = str("name").orEmpty()
                str("date")?.let { lines.add(Line("日期", it)) }
                str("calendar")?.let { lines.add(Line("历法", if (it == "lunar") "农历" else "阳历")) }
                int("remind_days_before")?.takeIf { it > 0 }?.let { lines.add(Line("提醒", "提前 $it 天")) }
                str("next_solar_date")?.let { lines.add(Line("下次", it)) }
            }
            "place" -> {
                text = str("name").orEmpty()
                str("visit_date")?.let { lines.add(Line("到访", it)) }
                str("description")?.let { lines.add(Line("描述", it)) }
            }
            "reaction" -> {
                rec("post")?.let { text = it.text.ifEmpty { "动态 #${it.id}" } }
                (obj["like"] as? JsonObject)?.let { o ->
                    change = PreviewView.Change(
                        "点赞",
                        (o["from"] as? JsonPrimitive)?.content ?: "未赞",
                        (o["to"] as? JsonPrimitive)?.content ?: "已赞",
                    )
                }
                (obj["favorite"] as? JsonObject)?.let { o ->
                    change = PreviewView.Change(
                        "收藏",
                        (o["from"] as? JsonPrimitive)?.content ?: "未收藏",
                        (o["to"] as? JsonPrimitive)?.content ?: "已收藏",
                    )
                }
            }
            "delete" -> {
                val arr = obj["items"] as? JsonArray
                if (arr != null && arr.isNotEmpty()) {
                    items = arr.mapNotNull { (it as? JsonObject)?.let { o -> parseRecord(o) } }
                    itemCount = int("count") ?: items.size
                } else {
                    text = str("text").orEmpty()
                    str("date")?.let { lines.add(Line("日期", it)) }
                }
            }
        }
        // 段序与阅读顺序一致：正文 → 媒体 → 元信息 → 变更 → 条目
        val views = buildList {
            if (text.isNotBlank()) add(PreviewView.Text(text))
            if (media.isNotEmpty() || mediaCount > 0) add(PreviewView.Media(media, mediaCount))
            if (lines.isNotEmpty()) add(PreviewView.Lines(lines))
            change?.let { add(it) }
            if (items.isNotEmpty()) add(PreviewView.Items(items, itemCount))
        }
        return CardPreview(kind = kind, views = views)
    }

    private fun parseRecord(o: JsonObject): UiRecord = UiRecord(
        id = (o["id"] as? JsonPrimitive)?.content?.toLongOrNull() ?: 0,
        kind = (o["kind"] as? JsonPrimitive)?.content.orEmpty(),
        text = (o["text"] as? JsonPrimitive)?.content.orEmpty(),
        date = (o["date"] as? JsonPrimitive)?.content.orEmpty(),
        thumb_url = (o["thumb_url"] as? JsonPrimitive)?.content.orEmpty(),
        badge = (o["badge"] as? JsonPrimitive)?.content.orEmpty(),
    )

    /** 意图 → 中文动词（确认卡标题） */
    fun intentLabel(intent: String): String = when (intent) {
        "propose_post" -> "发布动态"
        "propose_comment" -> "发表评论"
        "propose_album" -> "新建相册"
        "propose_album_members" -> "调整相册"
        "propose_anniversary" -> "新建纪念日"
        "propose_place" -> "记录地点"
        "propose_reaction" -> "点赞收藏"
        "propose_delete" -> "删除内容"
        else -> "待确认操作"
    }
}
