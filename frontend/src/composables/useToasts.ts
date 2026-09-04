import { ref } from 'vue'

export interface Toast {
  id: number
  text: string
  kind: 'info' | 'error'
}

const toasts = ref<Toast[]>([])
let counter = 0

export function useToasts(): {
  toasts: typeof toasts
  push: (text: string, kind?: Toast['kind']) => void
} {
  const push = (text: string, kind: Toast['kind'] = 'info'): void => {
    const id = ++counter
    toasts.value.push({ id, text, kind })
    setTimeout(() => {
      toasts.value = toasts.value.filter((t) => t.id !== id)
    }, 4000)
  }
  return { toasts, push }
}
