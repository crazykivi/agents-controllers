import { onUnmounted, ref } from 'vue'
import { api } from '../services/api'
import type { Approval } from '../services/types'

// Глобальный реестр вопросов агентов (y/n), опрашивается /api/approvals.
// Панель решений рисуется в App.vue поверх интерфейса.
const pending = ref<Approval[]>([])
let timer: ReturnType<typeof setInterval> | null = null
let refs = 0

async function poll(): Promise<void> {
  try {
    pending.value = await api.listApprovals()
  } catch {
    // сервер недоступен — оставляем как есть
  }
}

export function useApprovals(): {
  pending: typeof pending
  resolve: (id: string, action: 'allow' | 'deny') => Promise<void>
} {
  refs += 1
  if (!timer) {
    void poll()
    timer = setInterval(poll, 2000)
  }
  onUnmounted(() => {
    refs -= 1
    if (refs <= 0 && timer) {
      clearInterval(timer)
      timer = null
    }
  })
  return {
    pending,
    resolve: async (id, action) => {
      await api.resolveApproval(id, action)
      pending.value = pending.value.filter((a) => a.id !== id)
    },
  }
}
