<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'

/**
 * 带候选列表的输入框：可打字过滤、可点选，也保留手填。
 *
 * 为什么不用原生 `datalist`：它的面板由浏览器绘制，白底直角、无 hover 态，改不到；
 * 且点输入框不展开（只认打字或末端那个极小箭头），不符合「下拉」的交互预期。
 */
const props = withDefaults(
  defineProps<{
    modelValue: string
    options: string[]
    placeholder?: string
    id?: string
    /** 透传给内部输入框，保住原生表单校验态 */
    required?: boolean
  }>(),
  { placeholder: '', id: undefined, required: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const root = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const open = ref(false)
const highlight = ref(-1)

/** 面板高度的上限；实际还会被可用空间压小 */
const PANEL_MAX_H = 240
/**
 * 面板用 fixed 定位、坐标自己算：`.modal-body` 是 `overflow:auto`，
 * 用绝对定位会被它裁掉（实测 240px 的面板只剩 189px）。
 */
const pos = ref({ left: 0, top: 0, width: 0, maxH: PANEL_MAX_H, up: false })

const uid = useId()
const panelId = `combobox-panel-${uid}`
const optionId = (i: number) => `${panelId}-opt-${i}`

const filtered = computed(() => {
  const q = props.modelValue.trim().toLowerCase()
  if (!q) return props.options
  return props.options.filter((o) => o.toLowerCase().includes(q))
})

function place() {
  const el = input.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const below = window.innerHeight - r.bottom - 8
  const above = r.top - 8
  // 下方放不下且上方更宽裕时向上展开——否则矮视口下末几项会落到视口外
  const up = below < PANEL_MAX_H && above > below
  pos.value = {
    left: r.left,
    top: up ? r.top - 4 : r.bottom + 4,
    width: r.width,
    maxH: Math.max(120, Math.min(PANEL_MAX_H, up ? above : below)),
    up,
  }
}

function show() {
  if (props.options.length === 0) return
  place()
  open.value = true
  highlight.value = Math.max(0, filtered.value.indexOf(props.modelValue))
}

function close() {
  open.value = false
  highlight.value = -1
}

function pick(v: string) {
  emit('update:modelValue', v)
  close()
  input.value?.focus()
}

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
  // modelValue 要等父级回流才更新，`filtered` 此刻还是旧值，故等下一 tick 再判。
  // 位置不重算：输入框宽度不随打字变；`open` 仍要置真——候选是取回后才有的，
  // 聚焦时为空、之后打字必须能看到面板。
  nextTick(() => {
    if (filtered.value.length === 0) {
      close()
      return
    }
    open.value = true
    highlight.value = Math.max(0, filtered.value.indexOf(props.modelValue))
  })
}

function onKeydown(e: KeyboardEvent) {
  const list = filtered.value
  if (e.key === 'Escape') {
    if (open.value) {
      e.stopPropagation() // 别让 Esc 穿透到弹窗去关掉整个表单
      close()
    }
    return
  }
  const step = e.key === 'ArrowDown' ? 1 : e.key === 'ArrowUp' ? -1 : 0
  if (!open.value) {
    if (step && list.length > 0) {
      e.preventDefault()
      show()
    }
    return
  }
  if (list.length === 0) return
  if (step) {
    e.preventDefault()
    highlight.value = (highlight.value + step + list.length) % list.length
  } else if (e.key === 'Enter' && highlight.value >= 0) {
    e.preventDefault()
    pick(list[highlight.value])
  }
}

/** 点面板外关闭（面板已 Teleport 到 body，不在 root 里，故两侧都要判） */
function onDocClick(e: MouseEvent) {
  const t = e.target as Node
  if (root.value?.contains(t) || panel.value?.contains(t)) return
  close()
}

// 位置依赖视口：滚动/resize 时跟手
function onViewportChange() {
  if (open.value) place()
}

onMounted(() => {
  document.addEventListener('mousedown', onDocClick)
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocClick)
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
})

// 候选清空时收起：留着 open 会让下一批候选到达时面板自己弹出来
watch(
  () => props.options.length,
  (n) => {
    if (n === 0) close()
  },
)
</script>

<template>
  <div ref="root" class="combobox">
    <input
      ref="input"
      :id="id"
      :value="modelValue"
      class="form-input"
      type="text"
      autocomplete="off"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="open"
      :aria-controls="panelId"
      :aria-activedescendant="open && highlight >= 0 ? optionId(highlight) : undefined"
      :placeholder="placeholder"
      :required="required"
      @input="onInput"
      @focus="show"
      @keydown="onKeydown"
    />

    <Teleport to="body">
      <div
        v-if="open && filtered.length > 0"
        :id="panelId"
        ref="panel"
        class="combobox-panel"
        role="listbox"
        :style="{
          left: pos.left + 'px',
          top: pos.top + 'px',
          width: pos.width + 'px',
          maxHeight: pos.maxH + 'px',
          transform: pos.up ? 'translateY(-100%)' : 'none',
        }"
      >
        <div
          v-for="(o, i) in filtered"
          :id="optionId(i)"
          :key="o"
          class="combobox-option"
          :class="{ on: i === highlight }"
          role="option"
          :aria-selected="i === highlight"
          @mousedown.prevent="pick(o)"
          @mousemove="highlight = i"
        >
          {{ o }}
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* 面板虽 Teleport 到 body，作用域属性仍打在元素上，故 scoped 生效（与 AppModal 同款）。
   视觉与 `.card` / `.modal-panel` 同一套 token；z-index 150 落在遮罩 100 与 toast 200 之间。 */
.combobox {
  position: relative;
  flex: 1;
  min-width: 0;
}
.combobox-panel {
  position: fixed;
  z-index: 150;
  overflow-y: auto;
  padding: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
}
.combobox-option {
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  font-size: 13px;
  color: var(--color-text-primary);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.combobox-option.on {
  background: var(--color-accent-light);
}
</style>
