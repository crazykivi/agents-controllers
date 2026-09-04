<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { TaskMode } from '../services/types'
import { fmtDuration } from '../utils/format'
import { useToasts } from '../composables/useToasts'
import { useAppStore } from '../composables/useAppStore'
import { usePanelResize } from '../composables/usePanelResize'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const router = useRouter()
const { push } = useToasts()
const store = useAppStore()

const agents = store.agents
const tasks = store.tasks

// --- Перетаскиваемый сплит форма/список (сохраняется в localStorage) ---
const leftEl = ref<HTMLElement | null>(null)
const { size: leftWidth, dividerProps: splitSash } = usePanelResize('tasks-split', leftEl, {
  min: 280,
  max: Math.max(520, Math.floor(window.innerWidth * 0.75)),
  initial: 560,
  side: 'start',
  axis: 'x',
})

async function load(): Promise<void> {
  await store.refresh()
}
onMounted(load)

const form = reactive({
  title: '',
  description: '',
  agentIds: [] as string[],
  mode: 'sequential' as TaskMode,
  workdir: '',
  sharedDir: '',
})
const creating = ref(false)

async function create(): Promise<void> {
  if (!form.agentIds.length) {
    push('выберите хотя бы одного агента', 'error')
    return
  }
  creating.value = true
  try {
    const t = await api.createTask({
      title: form.title,
      description: form.description,
      agent_ids: form.agentIds,
      mode: form.mode,
      workdir: form.workdir.trim(),
      shared_dir: form.sharedDir.trim(),
    })
    router.push({ name: 'task', params: { id: t.id } })
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    creating.value = false
  }
}

function agentNames(ids: string[]): string {
  return ids
    .map((id) => agents.value.find((a) => a.id === id)?.name ?? id)
    .join(', ')
}
</script>

<template>
  <section class="flex min-h-full">
    <!-- Left: create form -->
    <div ref="leftEl" class="scroll-thin min-w-0 shrink-0 overflow-y-auto" :style="{ width: leftWidth + 'px' }">
      <div class="panel-title border-b border-vsc-border">Новая задача для crew</div>
      <form class="grid gap-4 px-5 py-4" @submit.prevent="create">
        <div>
          <label class="label" for="t-title">Название *</label>
          <input id="t-title" v-model="form.title" class="input" required maxlength="200" />
        </div>
        <div>
          <label class="label" for="t-desc">Описание задачи *</label>
          <textarea id="t-desc" v-model="form.description" class="input" rows="6" required />
        </div>
        <fieldset>
          <legend class="label">Режим выполнения</legend>
          <div class="grid gap-1.5 text-[13px]">
            <label class="flex cursor-pointer items-center gap-2">
              <input v-model="form.mode" type="radio" name="mode" value="sequential" class="checkbox" />
              <span><span class="font-medium">последовательный</span> — цепочка, следующий видит отчёт предыдущего</span>
            </label>
            <label class="flex cursor-pointer items-center gap-2">
              <input v-model="form.mode" type="radio" name="mode" value="parallel" class="checkbox" />
              <span><span class="font-medium">параллельный</span> — общий план + свои потоки</span>
            </label>
          </div>
        </fieldset>
        <div>
          <label class="label" for="t-workdir">Рабочая директория задачи (песочница) *</label>
          <input id="t-workdir" v-model="form.workdir" class="input font-mono" required placeholder="D:/repos/my-project" />
          <p class="mt-1 text-[11px] text-vsc-muted">
            Все выбранные агенты будут работать только в этой папке — их собственные
            директории игнорируются, выход за пределы папки заблокирован (--subtree-only).
          </p>
        </div>
        <div v-if="form.mode === 'parallel'">
          <label class="label" for="t-shared">Общая папка для плана и отчётов (пусто = директория задачи)</label>
          <input id="t-shared" v-model="form.sharedDir" class="input font-mono" placeholder="D:/repos/shared" />
          <p class="mt-1 text-[11px] text-vsc-muted">
            Туда попадут AGENTS_PLAN.md (план координатора) и status/&lt;агент&gt;.md (чек-листы отчётов).
          </p>
        </div>
        <fieldset>
          <legend class="label">Исполнители</legend>
          <div v-if="!agents.length" class="text-[13px] text-vsc-muted">
            Сначала создайте агентов на вкладке «Агенты».
          </div>
          <label
            v-for="a in agents"
            :key="a.id"
            class="flex cursor-pointer items-center gap-2 py-0.5 text-[13px]"
          >
            <input v-model="form.agentIds" type="checkbox" :value="a.id" class="checkbox" />
            <span
              class="h-2 w-2 shrink-0 rounded-full"
              :class="a.status === 'running' ? 'bg-vsc-green' : 'bg-vsc-gray'"
            />
            <span class="font-medium">{{ a.name }}</span>
            <span class="truncate text-[11px] text-vsc-muted">{{ a.role || a.workdir }}</span>
          </label>
        </fieldset>
        <div>
          <button class="btn-primary" type="submit" :disabled="creating">
            <VsIcon name="run" :size="13" />
            {{ creating ? 'Запуск…' : 'Запустить crew' }}
          </button>
        </div>
      </form>
    </div>

    <!-- Split sash -->
    <div
      class="group relative z-10 w-1 shrink-0 cursor-col-resize touch-none select-none"
      v-bind="splitSash"
    >
      <div class="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-vsc-border group-hover:bg-vsc-cyan" />
    </div>

    <!-- Right: task list -->
    <div class="scroll-thin min-w-0 flex-1 overflow-y-auto">
      <div class="panel-title border-b border-vsc-border">Задачи</div>
      <div v-if="!tasks.length" class="px-5 py-10 text-center text-sm text-vsc-muted">
        Задач пока нет.
      </div>
      <ul>
        <li v-for="t in tasks" :key="t.id" class="border-b border-vsc-border last:border-b-0">
          <button
            class="flex w-full items-center gap-3 px-5 py-2.5 text-left hover:bg-vsc-hover"
            @click="router.push({ name: 'task', params: { id: t.id } })"
          >
            <StatusBadge :status="t.status" />
            <span class="min-w-0 flex-1 truncate text-[13px] font-medium">{{ t.title }}</span>
            <span v-if="t.mode === 'parallel'" class="rounded bg-vsc-cyan-dim px-1.5 text-[11px] text-vsc-cyan">∥</span>
            <span class="hidden max-w-48 truncate text-[11px] text-vsc-muted md:inline">{{ agentNames(t.agent_ids) }}</span>
            <span class="shrink-0 font-mono text-[11px] text-vsc-muted">{{ fmtDuration(t.started_at, t.finished_at) }}</span>
          </button>
        </li>
      </ul>
    </div>
  </section>
</template>
