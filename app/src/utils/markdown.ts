// AI 回复 Markdown 渲染：markdown-it 解析 + DOMPurify 消毒后再 v-html。
// 仅助手气泡使用；用户/工具气泡与确认卡片维持纯文本。
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

// 消毒白名单之外一律剥除（html:false 已挡内联 HTML，此处兜 linkify 产出的 a 标签等）
const PURIFY_CFG = {
  ALLOWED_TAGS: [
    'p', 'br', 'strong', 'em', 'del', 's', 'code', 'pre', 'blockquote',
    'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr',
    'a', 'span', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
  ],
  ALLOWED_ATTR: ['href', 'target', 'rel'],
}

export function renderMarkdown(src: string): string {
  if (!src) return ''
  const html = md.render(src)
  const clean = DOMPurify.sanitize(html, PURIFY_CFG)
  // 外链新窗口打开且不带 opener（防反向 tabnabbing）
  return clean.replace(/<a /g, '<a target="_blank" rel="noopener noreferrer" ')
}
