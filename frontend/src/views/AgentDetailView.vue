<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { Agent, LogEvent } from '../services/types'
import { useEventStream } from '../composables/useEventStream'
import { useToasts } from '../composables/useToasts'
import LogConsole from '../components/LogConsole.vue'
import GitPanel from '../components/GitPanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const props = defineProps<{ id: string }>()
const { push } = useToasts()
const router = useRouter()

const agent = ref<Agent | null>(null)
const events = ref<LogEvent[]>([])
const draft = ref('')
const sending = ref(false)
const bottomView = ref<'terminal' | 'git'>('terminal')
const gitKey = ref(0)

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
    // история может быть пуста
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

async function undo(): Promise<void> {
  if (!agent.value) return
  try {
    await api.agentUndo(agent.value.id)
    push('отправлено /undo')
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

function goBack(): void {
  router.push('/')
}
</script>

<template>
  <section v-if="agent" class="flex h-full flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b border-vsc-border px-4 py-2 text-[13px]">
      <button class="text-vsc-muted hover:text-vsc-text" @click="goBack">Агенты</button>
      <VsIcon name="chevron" :size="12" class="text-vsc-muted" />
      <span class="font-medium">{{ agent.name }}</span>
      <StatusBadge :status="agent.status" />
      <span class="hidden truncate font-mono text-[11px] text-vsc-muted md:inline">{{ agent.workdir }}</span>
      <div class="ml-auto flex items-center gap-1.5">
        <button
          class="grid h-7 w-7 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text disabled:opacity-40"
          :disabled="!running"
          title="Undo последнего изменения (aider /undo)"
          @click="undo"
        >
          <VsIcon name="refresh" :size="14" />
        </button>
        <button class="btn-primary !py-0.5" @click="toggle">
          <VsIcon :name="running ? 'stop' : 'play'" :size="12" />
          {{ running ? 'Остановить' : 'Запустить' }}
        </button>
      </div>
    </div>

    <!-- Bottom panel switch: terminal / git -->
    <div class="flex h-9 shrink-0 items-stretch border-b border-vsc-border bg-vsc-chrome">
      <button
        class="px-4 text-[12px]"
        :class="bottomView === 'terminal' ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="bottomView = 'terminal'"
      >
        Терминал
      </button>
      <button
        class="px-4 text-[12px]"
        :class="bottomView === 'git' ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="bottomView = 'git'; gitKey++"
      >
        Git
      </button>
    </div>

    <template v-if="bottomView === 'terminal'">
      <LogConsole :events="events" />
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
    </template>

    <GitPanel
      v-else
      class="min-h-0 flex-1"
      :fetch-status="() => api.agentGitStatus(agent!.id)"
      :fetch-diff="() => api.agentGitDiff(agent!.id)"
      :refresh-key="gitKey"
    />
  </section>

  <div v-else class="flex h-full items-center justify-center text-sm text-vsc-muted">
    Агент не найден.
  </div>
</template>
