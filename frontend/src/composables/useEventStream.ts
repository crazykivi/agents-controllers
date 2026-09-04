import { onMounted, onUnmounted, ref, type Ref } from 'vue'
import { eventsUrl } from '../services/api'
import type { LogEvent } from '../services/types'

type Handler = (e: LogEvent) => void

const connected = ref(false)
const handlers = new Set<Handler>()
let source: EventSource | null = null
let refs = 0

function ensure(): void {
  if (source) return
  source = new EventSource(eventsUrl)
  source.onopen = () => {
    connected.value = true
  }
  source.onerror = () => {
    connected.value = false
  }
  source.onmessage = (m: MessageEvent<string>) => {
    try {
      const e = JSON.parse(m.data) as LogEvent
      handlers.forEach((h) => h(e))
    } catch {
      /* повреждённый кадр игнорируем */
    }
  }
}

// Синглтон EventSource с refcount: соединение открывается с первым
// подписчиком и закрывается последним.
export function useEventStream(onEvent: Handler): { connected: Ref<boolean> } {
  onMounted(() => {
    refs += 1
    ensure()
    handlers.add(onEvent)
  })
  onUnmounted(() => {
    handlers.delete(onEvent)
    refs -= 1
    if (refs <= 0 && source) {
      source.close()
      source = null
      connected.value = false
      refs = 0
    }
  })
  return { connected }
}
