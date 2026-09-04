import { computed, ref, watch } from 'vue'
import type { Ref } from 'vue'

const PREFIX = 'ac-panel-'

export interface PanelOpts {
  min: number
  max: number
  initial: number
  /** 'start' — панель слева/сверху (якорь — левый/верхний край), 'end' — справа/снизу */
  side: 'start' | 'end'
  axis: 'x' | 'y'
}

const clamp = (n: number, min: number, max: number): number => Math.min(max, Math.max(min, n))

/**
 * Размер перетаскиваемой панели с сохранением в localStorage.
 * Возвращает ref размера и props-объект для «сашки» (v-bind="dividerProps").
 */
export function usePanelResize(
  key: string,
  panel: Ref<HTMLElement | null>,
  opts: PanelOpts,
): { size: Ref<number>; dividerProps: Record<string, (e: PointerEvent) => void> } {
  const size = ref(opts.initial)
  try {
    const raw = localStorage.getItem(PREFIX + key)
    if (raw !== null) {
      const n = Number(raw)
      if (Number.isFinite(n)) size.value = clamp(n, opts.min, opts.max)
    }
  } catch {
    // ignore
  }

  let dragging = false

  function down(e: PointerEvent): void {
    dragging = true
    const el = e.currentTarget as HTMLElement | null
    try {
      el?.setPointerCapture(e.pointerId)
    } catch {
      // ignore
    }
    document.body.style.userSelect = 'none'
    document.body.style.cursor = opts.axis === 'x' ? 'col-resize' : 'row-resize'
    e.preventDefault()
  }

  function move(e: PointerEvent): void {
    if (!dragging || !panel.value) return
    const r = panel.value.getBoundingClientRect()
    const pos = opts.axis === 'x' ? e.clientX : e.clientY
    const anchor =
      opts.side === 'start'
        ? opts.axis === 'x'
          ? r.left
          : r.top
        : opts.axis === 'x'
          ? r.right
          : r.bottom
    const next = opts.side === 'start' ? pos - anchor : anchor - pos
    size.value = clamp(next, opts.min, opts.max)
  }

  function up(e: PointerEvent): void {
    if (!dragging) return
    dragging = false
    const el = e.currentTarget as HTMLElement | null
    try {
      el?.releasePointerCapture(e.pointerId)
    } catch {
      // ignore
    }
    document.body.style.userSelect = ''
    document.body.style.cursor = ''
    try {
      localStorage.setItem(PREFIX + key, String(size.value))
    } catch {
      // ignore
    }
  }

  return {
    size,
    dividerProps: { onPointerdown: down, onPointermove: move, onPointerup: up, onPointercancel: up },
  }
}
