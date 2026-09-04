<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { LogEvent, Task } from '../services/types'
import { fmtDuration } from '../utils/format'
import { useEventStream } from '../composables/useEventStream'
import { useToasts } from '../composables/useToasts'
import { usePanelResize } from '../composables/usePanelResize'
import LogConsole from '../components/LogConsole.vue'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const props = defineProps<{ id: string }>()
const { push } = useToasts()
const router = useRouter()

const task = ref<Task | null>(null)
const events = ref<LogEvent[]>([])
const filter = ref<string>('all')

// --- Перетаскиваемая панель результата (сохраняется в localStorage) ---
const resultEl = ref<HTMLElement | null>(null)
const { size: resultHeight, dividerProps: resultSash } = usePanelResize(
  'task-result',
  resultEl,
  { min: 80, max: 600, initial: 180, side: 'end', axis: 'y' },
)

useEventStream((e) => {
  if (e.source === 'crew' && e.ref === props.id) {
    events.value.push(e)
    if (events.value.length > 3000) events.value.splice(0, 1000)
    if (e.kind === 'status' || e.kind === 'error' || e.kind === 'result') void load()
  }
})

async function load(): Promise<void> {
  try {
    task.value = await api.getTask(props.id)
  } catch {
    task.value = null
  }
}

onMounted(async () => {
  await load()
  try {
    events.value = await api.taskLogs(props.id, 1000)
  } catch {
    /* пусто */
  }
})

const agentTabs = computed(() => {
  const names = new Set<string>()
  for (const e of events.value) if (e.agent) names.add(e.agent)
  return [...names]
})

const shown = computed(() =>
  filter.value === 'all' ? events.value : events.value.filter((e) => e.agent === filter.value),
)

const running = computed(() => task.value?.status === 'running')

async function cancel(): Promise<void> {
  if (!task.value) return
  try {
    await api.cancelTask(task.value.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

function goBack(): void {
  router.push('/tasks')
}
</script>

<template>
  <section v-if="task" class="flex h-full flex-col">
    <!-- Header -->
    <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-vsc-border px-4 py-2 text-[13px]">
      <button class="text-vsc-muted hover:text-vsc-text" @click="goBack">Задачи</button>
      <VsIcon name="chevron" :size="12" class="text-vsc-muted" />
      <span class="font-medium">{{ task.title }}</span>
      <StatusBadge :status="task.status" />
      <span class="rounded bg-vsc-btn2 px-1.5 py-0.5 text-[11px] text-vsc-muted">
        {{ task.mode === 'parallel' ? 'параллельно' : 'последовательно' }}
      </span>
      <span
        v-if="task.workdir"
        class="hidden max-w-64 truncate font-mono text-[11px] text-vsc-cyan md:inline"
        title="Песочница задачи"
      >
        {{ task.workdir }}
      </span>
      <span class="font-mono text-[11px] text-vsc-muted">{{ fmtDuration(task.started_at, task.finished_at) }}</span>
      <button v-if="running" class="btn-danger ml-auto !py-0.5" @click="cancel">Отменить</button>
    </div>

    <p v-if="task.error" class="shrink-0 bg-vsc-red-dim px-4 py-1.5 text-xs text-vsc-red">
      {{ task.error }}
    </p>

    <!-- Agent filter tabs like editor tabs -->
    <div class="flex shrink-0 items-stretch border-b border-vsc-border bg-vsc-chrome" role="tablist" aria-label="Фильтр по агентам">
      <button
        class="px-3 text-[12px]"
        :class="filter === 'all' ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="filter = 'all'"
      >
        все
      </button>
      <button
        v-for="name in agentTabs"
        :key="name"
        class="px-3 text-[12px]"
        :class="filter === name ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="filter = name"
      >
        {{ name }}
      </button>
    </div>

    <!-- Terminal -->
    <LogConsole :events="shown" />

    <!-- Result panel (resizable) -->
    <template v-if="task.result">
      <div
        class="group relative z-10 h-1 w-full shrink-0 cursor-row-resize touch-none select-none"
        v-bind="resultSash"
      >
        <div class="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-vsc-border group-hover:bg-vsc-cyan" />
      </div>
      <div ref="resultEl" class="shrink-0 overflow-hidden bg-vsc-term" :style="{ height: resultHeight + 'px' }">
        <div class="scroll-thin h-full overflow-auto">
          <div class="px-4 pt-2 text-[11px] font-semibold uppercase tracking-widest text-vsc-green">Результат</div>
          <pre class="whitespace-pre-wrap px-4 pb-3 pt-1 font-mono text-xs text-vsc-text">{{ task.result }}</pre>
        </div>
      </div>
    </template>
  </section>

  <div v-else class="flex h-full items-center justify-center text-sm text-vsc-muted">
    Задача не найдена.
  </div>
</template>
