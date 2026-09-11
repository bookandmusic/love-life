import { ref } from 'vue'

// 全屏媒体查看器状态（AlbumsView 时间轴 / AlbumDetailView 相册详情共用：开关语义完全一致，
// 「移除/跳动态」等差异由各自视图挂在 MediaViewer 的事件上）
export function useViewer() {
  const viewer = ref<{ index: number } | null>(null)
  function openViewer(index: number) {
    viewer.value = { index }
  }
  function closeViewer() {
    viewer.value = null
  }
  return { viewer, openViewer, closeViewer }
}
