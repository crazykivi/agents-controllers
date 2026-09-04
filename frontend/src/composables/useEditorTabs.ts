import { reactive } from 'vue'

const PREFIX = 'ac-editor-tabs'

/**
 * Редакторские вкладки в стиле VS Code.
 * Инвариант: в state.tabs закреплённые всегда идут впереди обычных.
 * Все изменения — мутациями на месте (splice), чтобы не рвать реактивные ссылки.
 */
interface Stored {
  tabs: string[]
  pinned: string[]
  base: string[] // порядок базовых вкладок '/' и '/tasks'
}

const TAB_RE = /^\/(agents|tasks)\/[A-Za-z0-9]+$/
const BASE = ['/', '/tasks', '/settings']

function load(): Stored {
  try {
    const raw = localStorage.getItem(PREFIX)
    if (raw) {
      const v = JSON.parse(raw) as Stored
      if (Array.isArray(v.tabs) && Array.isArray(v.pinned)) {
        const base =
          Array.isArray(v.base) &&
          BASE.every((b) => v.base.includes(b)) &&
          v.base.every((b) => BASE.includes(b))
            ? [...v.base]
            : [...BASE]
        const pinned = v.pinned.filter((t) => typeof t === 'string' && TAB_RE.test(t))
        const tabs = v.tabs.filter((t) => typeof t === 'string' && TAB_RE.test(t))
        const p = tabs.filter((t) => pinned.includes(t))
        const u = tabs.filter((t) => !pinned.includes(t))
        for (const t of pinned) if (!p.includes(t)) p.push(t)
        return { tabs: [...p, ...u], pinned: [...p], base }
      }
    }
  } catch {
    // ignore
  }
  return { tabs: [], pinned: [], base: [...BASE] }
}

const state = reactive(load())

function persist(): void {
  try {
    localStorage.setItem(
      PREFIX,
      JSON.stringify({ tabs: state.tabs, pinned: state.pinned, base: state.base }),
    )
  } catch {
    // ignore
  }
}

export function useEditorTabs(): {
  tabs: string[]
  base: string[]
  isPinned: (to: string) => boolean
  open: (to: string) => void
  close: (to: string) => void
  closeOthers: (to: string) => string[]
  closeAll: () => string[]
  closeRight: (to: string) => string[]
  togglePin: (to: string) => void
  /** Перенести вкладку from на позицию перед/после вкладки to (с учётом pinned-инварианта). */
  move: (from: string, to: string, before: boolean) => void
  /** Поменять местами две базовые вкладки (единственное их перемещение). */
  swapBase: (a: string, b: string) => void
} {
  const isPinned = (to: string): boolean => state.pinned.includes(to)

  function remove(paths: string[]): void {
    const keep = state.tabs.filter((t) => !paths.includes(t))
    state.tabs.splice(0, state.tabs.length, ...keep)
    const keepP = state.pinned.filter((t) => !paths.includes(t))
    state.pinned.splice(0, state.pinned.length, ...keepP)
    persist()
  }

  function open(to: string): void {
    if (!TAB_RE.test(to) || state.tabs.includes(to)) return
    state.tabs.push(to)
    persist()
  }

  function close(to: string): void {
    remove([to])
  }

  function closeOthers(to: string): string[] {
    const removed = state.tabs.filter((t) => t !== to && !isPinned(t))
    remove(removed)
    return removed
  }

  function closeAll(): string[] {
    const removed = state.tabs.filter((t) => !isPinned(t))
    remove(removed)
    return removed
  }

  function closeRight(to: string): string[] {
    const i = state.tabs.indexOf(to)
    if (i === -1) return []
    const removed = state.tabs.filter((t, j) => j > i && !isPinned(t))
    remove(removed)
    return removed
  }

  function togglePin(to: string): void {
    const i = state.tabs.indexOf(to)
    if (i === -1) return
    if (isPinned(to)) {
      // открепить: вкладка встаёт в начало обычной секции
      state.pinned.splice(state.pinned.indexOf(to), 1)
      state.tabs.splice(i, 1)
      const pinnedCount = state.tabs.filter((t) => state.pinned.includes(t)).length
      state.tabs.splice(pinnedCount, 0, to)
    } else {
      // закрепить: вкладка встаёт в конец закреплённой секции
      state.pinned.push(to)
      state.tabs.splice(i, 1)
      state.tabs.unshift(to)
    }
    persist()
  }

  function move(from: string, to: string, before: boolean): void {
    if (from === to) return
    const fi = state.tabs.indexOf(from)
    const ti = state.tabs.indexOf(to)
    if (fi === -1 || ti === -1) return
    const [tab] = state.tabs.splice(fi, 1)
    let idx = state.tabs.indexOf(to)
    if (!before) idx += 1
    // обычные вкладки не могут заезжать в закреплённую секцию и наоборот
    const pinnedCount = state.tabs.filter((t) => state.pinned.includes(t)).length
    idx = state.pinned.includes(tab) ? Math.min(idx, pinnedCount) : Math.max(idx, pinnedCount)
    state.tabs.splice(idx, 0, tab)
    persist()
  }

  function swapBase(a: string, b: string): void {
    const i = state.base.indexOf(a)
    const j = state.base.indexOf(b)
    if (i === -1 || j === -1 || i === j) return
    const tmp = state.base[i]
    state.base.splice(i, 1, state.base[j])
    state.base.splice(j, 1, tmp)
    persist()
  }

  return {
    tabs: state.tabs,
    base: state.base,
    isPinned,
    open,
    close,
    closeOthers,
    closeAll,
    closeRight,
    togglePin,
    move,
    swapBase,
  }
}
