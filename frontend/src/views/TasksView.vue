<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { TaskMode, TaskTemplate } from '../services/types'
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
const templates = ref<TaskTemplate[]>([])
const tplName = ref('')

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
  try {
    templates.value = await api.listTemplates()
  } catch {
    // не критично
  }
}
onMounted(load)

const form = reactive({
  title: '',
  description: '',
  agentIds: [] as string[],
  mode: 'sequential' as TaskMode,
  workdir: '',
  sharedDir: '',
  confirmPlan: false,
  dependsOn: [] as string[],
})
const creating = ref(false)
const busy = reactive<Record<string, boolean>>({})

const runnableDeps = computed(() =>
  tasks.value.filter(
    (t) =>
      t.status === 'pending' || t.status === 'running' || t.status === 'awaiting_approval',
  ),
)

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
      confirm_plan: form.confirmPlan,
      depends_on: form.dependsOn,
    })
    router.push({ name: 'task', params: { id: t.id } })
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    creating.value = false
  }
}

// --- Templates ---

async function saveAsTemplate(): Promise<void> {
  const name = tplName.value.trim()
  if (!name || !form.title.trim()) {
    push('нужны имя шаблона и название задачи', 'error')
    return
  }
  try {
    await api.createTemplateFromForm(name, {
      title: form.title,
      description: form.description,
      agent_ids: form.agentIds,
      mode: form.mode,
      workdir: form.workdir.trim(),
      shared_dir: form.sharedDir.trim(),
      confirm_plan: form.confirmPlan,
    })
    await load()
    push(`шаблон «${name}» сохранён`)
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

async function loadTemplate(tpl: TaskTemplate): Promise<void> {
  form.title = tpl.title
  form.description = tpl.description
  form.agentIds = [...tpl.agent_ids]
  form.mode = tpl.mode === 'parallel' ? 'parallel' : 'sequential'
  form.workdir = tpl.workdir ?? ''
  form.sharedDir = tpl.shared_dir ?? ''
  form.confirmPlan = !!tpl.confirm_plan
  form.dependsOn = []
  push(`шаблон «${tpl.name}» загружен`)
}

async function removeTemplate(tpl: TaskTemplate): Promise<void> {
  try {
    await api.deleteTemplate(tpl.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}

// --- Task row actions ---

function canDelete(s: string): boolean {
  return s === 'done' || s === 'failed' || s === 'canceled'
}

async function remove(t: { id: string; title: string }): Promise<void> {
  if (!confirm(`Удалить задачу «${t.title}»? История и логи будут потеряны.`)) return
  busy[t.id] = true
  try {
    await api.deleteTask(t.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    busy[t.id] = false
  }
}

async function restart(t: { id: string; title: string }): Promise<void> {
  busy[t.id] = true
  try {
    const nt = await api.restartTask(t.id)
    push(`создана копия задачи`)
    router.push({ name: 'task', params: { id: nt.id } })
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    busy[t.id] = false
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

      <!-- Templates -->
      <div v-if="templates.length" class="border-b border-vsc-border px-5 py-3">
        <div class="label">Шаблоны</div>
        <div class="flex flex-wrap gap-1.5">
          <span
            v-for="tpl in templates"
            :key="tpl.id"
            class="group inline-flex items-center gap-1 border border-vsc-border bg-vsc-btn2 text-[12px]"
          >
            <button class="py-0.5 pl-2 text-vsc-text hover:text-vsc-cyan" :title="tpl.description" @click="loadTemplate(tpl)">
              {{ tpl.name }}
            </button>
            <button
              class="grid h-5 w-5 place-items-center text-vsc-muted hover:text-vsc-red"
              :aria-label="`Удалить шаблон ${tpl.name}`"
              @click="removeTemplate(tpl)"
            >
              <VsIcon name="close" :size="9" />
            </button>
          </span>
        </div>
      </div>

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
        <label v-if="form.mode === 'parallel'" class="flex cursor-pointer items-start gap-2 text-[13px]">
          <input v-model="form.confirmPlan" type="checkbox" class="checkbox mt-0.5" />
          <span>
            <span class="font-medium">Сначала показать план (dry-run)</span>
            <span class="block text-[11px] text-vsc-muted">
              Координатор составит план и остановится: ты его посмотришь и подтвердишь — только
              после этого агенты начнут работать.
            </span>
          </span>
        </label>
        <fieldset v-if="runnableDeps.length">
          <legend class="label">Ожидать завершения задач (очередь)</legend>
          <label
            v-for="t in runnableDeps"
            :key="t.id"
            class="flex cursor-pointer items-center gap-2 py-0.5 text-[13px]"
          >
            <input v-model="form.dependsOn" type="checkbox" :value="t.id" class="checkbox" />
            <StatusBadge :status="t.status" />
            <span class="min-w-0 truncate">{{ t.title }}</span>
          </label>
          <p class="mt-1 text-[11px] text-vsc-muted">
            Задача стартует автоматически, когда все отмеченные завершатся. Если какая-то
            провалится — задача будет отменена.
          </p>
        </fieldset>
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
        <div class="flex items-center gap-2">
          <button class="btn-primary" type="submit" :disabled="creating">
            <VsIcon name="run" :size="13" />
            {{ creating ? 'Создание…' : form.dependsOn.length ? 'Поставить в очередь' : 'Запустить crew' }}
          </button>
          <input
            v-model="tplName"
            class="input w-40"
            placeholder="имя шаблона"
            @keydown.enter.prevent
          />
          <button
            class="btn-secondary shrink-0"
            type="button"
            :disabled="!tplName.trim() || !form.title.trim()"
            title="Сохранить текущую форму как шаблон"
            @click="saveAsTemplate"
          >
            В шаблон
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
          <div class="group flex items-center gap-2 px-5 py-2 hover:bg-vsc-hover">
            <button
              class="flex min-w-0 flex-1 items-center gap-3 text-left"
              @click="router.push({ name: 'task', params: { id: t.id } })"
            >
              <StatusBadge :status="t.status" />
              <span class="min-w-0 truncate text-[13px] font-medium">{{ t.title }}</span>
              <span v-if="t.mode === 'parallel'" class="rounded bg-vsc-cyan-dim px-1.5 text-[11px] text-vsc-cyan">∥</span>
              <span v-if="t.depends_on?.length" class="hidden font-mono text-[10px] text-vsc-muted lg:inline" :title="agentNames(t.depends_on)">
                ⛓ {{ t.depends_on.length }}
              </span>
              <span class="ml-auto hidden max-w-48 truncate text-[11px] text-vsc-muted md:inline">{{ agentNames(t.agent_ids) }}</span>
              <span class="shrink-0 font-mono text-[11px] text-vsc-muted">{{ fmtDuration(t.started_at, t.finished_at) }}</span>
            </button>
            <div class="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
              <button
                class="grid h-6 w-6 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text"
                :disabled="busy[t.id]"
                title="Перезапустить копией"
                @click="restart(t)"
              >
                <VsIcon name="refresh" :size="13" />
              </button>
              <button
                class="grid h-6 w-6 place-items-center rounded text-vsc-muted hover:bg-vsc-red hover:text-white disabled:opacity-40"
                :disabled="busy[t.id] || !canDelete(t.status)"
                :title="canDelete(t.status) ? 'Удалить' : 'Сначала завершите или отмените задачу'"
                @click="remove(t)"
              >
                <VsIcon name="trash" :size="13" />
              </button>
            </div>
          </div>
        </li>
      </ul>
    </div>
  </section>
</template>
