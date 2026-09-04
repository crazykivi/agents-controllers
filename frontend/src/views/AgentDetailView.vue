<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { Agent, LogEvent } from '../services/types'
import { useEventStream } from '../composables/useEventStream'
import { useToasts } from '../composables/useToasts'
import { useAppStore } from '../composables/useAppStore'
import LogConsole from '../components/LogConsole.vue'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const props = defineProps<{ id: string }>()
const { push } = useToasts()
const router = useRouter()
const store = useAppStore()

const agent = ref<Agent | null>(null)
const events = ref<LogEvent[]>([])
const draft = ref('')
const sending = ref(false)

useEventStream((e) => {
  if (e.source === 'agent' && e.ref === props.id) {
    events.value.push(e)
    if (events.value.length > 2000) events.value.splice(0, 500)
    if (e.kind === 'status' || e.kind === 'error') void loadAgent()
  }
})

async function loadAgent(): Promise<void> {
  try {
    agent.value = await api.getAgent(props.id)
  } catch {
    agent.value = null
  }
}

onMounted(async () => {
  await loadAgent()
  if (!agent.value) {
    push('агент не найден', 'error')
    return
  }
  try {
    events.value = await api.agentLogs(props.id, 500)
  } catch {
    /* история может быть пуста */
  }
})
const running = computed(() => agent.value?.status === 'running')

async function toggle(): Promise<void> {
  if (!agent.value) return
  try {
    if (running.value) await api.stopAgent(agent.value.id)
    else await api.startAgent(agent.value.id)
    await loadAgent()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function send(): Promise<void> {
  const text = draft.value.trim()
  if (!text || !agent.value) return
  sending.value = true
  try {
    await api.sendInput(agent.value.id, text)
    draft.value = ''
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    sending.value = false
  }
}

function goBack(): void {
  router.push('/')
}
</script>

<template>
  <section v-if="agent" class="flex h-full flex-col">
    <!-- Breadcrumb header -->
    <div class="flex shrink-0 items-center gap-2 border-b border-vsc-border px-4 py-2 text-[13px]">
      <button class="text-vsc-muted hover:text-vsc-text" @click="goBack">Агенты</button>
      <VsIcon name="chevron" :size="12" class="text-vsc-muted" />
      <span class="font-medium">{{ agent.name }}</span>
      <StatusBadge :status="agent.status" />
      <span class="hidden truncate font-mono text-[11px] text-vsc-muted md:inline">{{ agent.workdir }}</span>
      <div class="ml-auto flex items-center gap-1.5">
        <button class="btn-primary !py-0.5" @click="toggle">
          <VsIcon :name="running ? 'stop' : 'play'" :size="12" />
          {{ running ? 'Остановить' : 'Запустить' }}
        </button>
      </div>
    </div>

    <!-- Terminal -->
    <LogConsole :events="events" />

    <!-- Input line like terminal prompt -->
    <div class="flex shrink-0 items-center gap-2 border-t border-vsc-border bg-vsc-term px-4 py-2">
      <span class="font-mono text-sm" :class="running ? 'text-vsc-green' : 'text-vsc-gray'">❯</span>
      <input
        v-model="draft"
        class="w-full bg-transparent font-mono text-[13px] text-vsc-text outline-none placeholder:text-vsc-muted"
        :disabled="!running"
        :placeholder="running ? 'Команда или сообщение в stdin aider (Enter — отправить)' : 'Сессия остановлена'"
        @keydown.enter.prevent="send"
      />
      <button
        class="btn-primary !py-0.5"
        :disabled="!running || sending || !draft.trim()"
        @click="send"
      >
        Отправить
      </button>
    </div>
  </section>

  <div v-else class="flex h-full items-center justify-center text-sm text-vsc-muted">
    Агент не найден.
  </div>
</template>
