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
import GitPanel from '../components/GitPanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const props = defineProps<{ id: string }>()
const { push } = useToasts()
const router = useRouter()

const task = ref<Task | null>(null)
const events = ref<LogEvent[]>([])
const filter = ref<string>('all')
const bottomView = ref<'terminal' | 'git'>('terminal')
const gitKey = ref(0)
const rollingBack = ref(false)

const resultEl = ref<HTMLElement | null>(null)
const { size: resultHeight, dividerProps: resultSash } = usePanelResize('task-result', resultEl, {
  min: 80,
  max: 600,
  initial: 180,
  side: 'end',
  axis: 'y',
})

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
    // пусто
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
const awaiting = computed(() => task.value?.status === 'awaiting_approval')

async function cancel(): Promise<void> {
  if (!task.value) return
  try {
    await api.cancelTask(task.value.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function approve(approve: boolean): Promise<void> {
  if (!task.value) return
  try {
    task.value = await api.approveTask(task.value.id, approve)
    push(approve ? 'план подтверждён — агенты работают' : 'задача отклонена')
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function restart(): Promise<void> {
  if (!task.value) return
  try {
    const nt = await api.restartTask(task.value.id)
    push('создана копия задачи')
    router.push({ name: 'task', params: { id: nt.id } })
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function removeTask(): Promise<void> {
  if (!task.value) return
  if (!confirm(`Удалить задачу «${task.value.title}»? История и логи будут потеряны.`)) return
  try {
    await api.deleteTask(task.value.id)
    router.push('/tasks')
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function rollback(): Promise<void> {
  if (!task.value?.base_dir || !task.value.base_sha) return
  const short = task.value.base_sha.slice(0, 8)
  if (!confirm(`Откатить все изменения в ${task.value.base_dir} к снапшоту ${short}? Незакоммиченные изменения будут потеряны.`)) return
  rollingBack.value = true
  try {
    task.value = await api.taskRollback(task.value.id)
    gitKey.value++
    push(`откачено к снапшоту ${short}`)
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    rollingBack.value = false
  }
}

function goBack(): void {
  router.push('/tasks')
}
</script>

<template>
  <section v-if="task" class="flex h-full flex-col">
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
      <div class="ml-auto flex items-center gap-1.5">
        <template v-if="awaiting">
          <button class="btn-primary !py-0.5" @click="approve(true)">Подтвердить план</button>
          <button class="btn-danger !py-0.5" @click="approve(false)">Отклонить</button>
        </template>
        <button
          v-if="task.base_sha"
          class="btn-secondary !py-0.5"
          :disabled="running || rollingBack"
          title="Откатить рабочую копию к git-снапшоту"
          @click="rollback"
        >
          <VsIcon name="refresh" :size="12" />
          Откат
        </button>
        <button
          class="grid h-6 w-6 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text"
          :disabled="running || awaiting"
          title="Перезапустить копией"
          @click="restart"
        >
          <VsIcon name="refresh" :size="14" />
        </button>
        <button
          class="grid h-6 w-6 place-items-center rounded text-vsc-muted hover:bg-vsc-red hover:text-white disabled:opacity-40"
          :disabled="running || awaiting"
          title="Удалить задачу"
          @click="removeTask"
        >
          <VsIcon name="trash" :size="14" />
        </button>
        <button v-if="running" class="btn-danger !py-0.5" @click="cancel">Отменить</button>
      </div>
    </div>

    <div
      v-if="awaiting"
      class="flex shrink-0 items-center gap-3 border-b border-vsc-border bg-vsc-btn2 px-4 py-2 text-[12px]"
    >
      <VsIcon name="x" :size="13" class="text-vsc-yellow" />
      <span class="font-medium">Координатор составил план и ждёт подтверждения</span>
      <div class="ml-auto flex gap-1.5">
        <button class="btn-primary !py-0.5" @click="approve(true)">Подтвердить и запустить</button>
        <button class="btn-danger !py-0.5" @click="approve(false)">Отклонить</button>
      </div>
    </div>

    <p v-if="task.error" class="shrink-0 bg-vsc-red-dim px-4 py-1.5 text-xs text-vsc-red">
      {{ task.error }}
    </p>

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
      <span
        v-if="task.base_sha"
        class="ml-2 self-center font-mono text-[10px] text-vsc-muted"
        title="HEAD на момент запуска задачи"
      >
        снапшот: {{ task.base_sha.slice(0, 8) }}
      </span>
    </div>

    <template v-if="bottomView === 'terminal'">
      <div
        class="flex shrink-0 items-stretch border-b border-vsc-border bg-vsc-chrome"
        role="tablist"
        aria-label="Фильтр по агентам"
      >
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

      <LogConsole :events="shown" />

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
    </template>

    <GitPanel
      v-else
      class="min-h-0 flex-1"
      :fetch-status="() => api.taskGitStatus(task!.id)"
      :fetch-diff="() => api.taskGitDiff(task!.id)"
      :refresh-key="gitKey"
    />
  </section>

  <div v-else class="flex h-full items-center justify-center text-sm text-vsc-muted">
    Задача не найдена.
  </div>
</template>
