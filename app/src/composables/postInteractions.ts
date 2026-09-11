import { api, errText } from '../api/client'
import type { Post } from '../types'

// 动态的点赞/收藏/评论：原先 PostsView（列表）与 PostDetailView（详情）各写一份逐行重复的实现，
// 差异只在错误出口与点赞副作用，故以回调解耦。（详情页已移除，列表页为唯一入口）
export function usePostInteractions(opts: { onError: (msg: string) => void; onLiked?: () => void }) {
  async function toggleLike(p: Post) {
    try {
      const res = await api<{ liked: boolean }>(`/api/v1/posts/${p.id}/like`, { method: 'POST' })
      p.liked = res.liked
      p.like_count += res.liked ? 1 : -1
      if (res.liked) opts.onLiked?.()
    } catch (e) {
      opts.onError(errText(e, '操作失败'))
    }
  }

  async function toggleFav(p: Post) {
    try {
      const res = await api<{ favorited: boolean }>(`/api/v1/posts/${p.id}/favorite`, { method: 'POST' })
      p.favorited = res.favorited
      p.favorite_count += res.favorited ? 1 : -1
    } catch (e) {
      opts.onError(errText(e, '操作失败'))
    }
  }

  /** 发表评论；成功返回 true（由调用方决定是否清空输入并重新拉取） */
  async function submitComment(p: Post, text: string): Promise<boolean> {
    const content = text.trim()
    if (!content) return false
    try {
      await api(`/api/v1/posts/${p.id}/comments`, { method: 'POST', body: JSON.stringify({ content }) })
      p.comment_count++
      return true
    } catch (e) {
      opts.onError(errText(e, '评论失败'))
      return false
    }
  }

  return { toggleLike, toggleFav, submitComment }
}
