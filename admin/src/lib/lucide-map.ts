/**
 * 动态图标映射：服务端数据里带 lucide 图标名（仪表盘统计卡 / 活动流），
 * 渲染时经此表转组件；未知名字回退到 FileText，不引第三方动态加载。
 */
import {
  AlertCircle,
  AlertTriangle,
  Brain,
  Calendar,
  CheckCircle2,
  FileText,
  Heart,
  Image,
  KeyRound,
  LayoutDashboard,
  MapPin,
  MessageCircle,
  MessageSquare,
  RefreshCw,
  ScrollText,
  Send,
  Trash2,
} from 'lucide-vue-next'
import type { Component } from 'vue'

export const lucideMap: Record<string, Component> = {
  'alert-circle': AlertCircle,
  'alert-triangle': AlertTriangle,
  brain: Brain,
  calendar: Calendar,
  'check-circle': CheckCircle2,
  'file-text': FileText,
  heart: Heart,
  image: Image,
  'key-round': KeyRound,
  'layout-dashboard': LayoutDashboard,
  'map-pin': MapPin,
  'message-circle': MessageCircle,
  'message-square': MessageSquare,
  'refresh-cw': RefreshCw,
  'scroll-text': ScrollText,
  send: Send,
  'trash-2': Trash2,
}

export function icon(name: string): Component {
  return lucideMap[name] ?? FileText
}
