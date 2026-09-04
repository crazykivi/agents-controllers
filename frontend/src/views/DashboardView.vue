<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../services/api'
import type { Agent, AgentInput, AgentPerms } from '../services/types'
import { useToasts } from '../composables/useToasts'
import { useAppStore } from '../composables/useAppStore'
import StatusBadge from '../components/StatusBadge.vue'
import VsIcon from '../components/VsIcon.vue'

const router = useRouter()
const { push } = useToasts()
const store = useAppStore()

const agents = store.agents
const loading = ref(true)
const busy = reactive<Record<string, boolean>>({})

async function load(): Promise<void> {
  try {
    await store.refresh()
  } catch (e) {
    push(`не удалось загрузить агентов: ${(e as Error).message}`, 'error')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
})

const defaultPerms = (): AgentPerms => ({ auto_yes: true, auto_commits: true, detect_urls: false })

const emptyForm = (): AgentInput & { flagsText: string } => ({
  name: '',
  workdir: '',
  model: '',
  flags: [],
  env: {},
  role: '',
  goal: '',
  backstory: '',
  perms: defaultPerms(),
  flagsText: '',
})

const showForm = ref(false)
const editingId = ref<string | null>(null)
const form = reactive<AgentInput & { flagsText: string }>(emptyForm())
const saving = ref(false)

function fillForm(a: Agent): void {
  Object.assign(form, {
    name: a.name,
    workdir: a.workdir,
    model: a.model ?? '',
    env: a.env ?? {},
    role: a.role ?? '',
    goal: a.goal ?? '',
    backstory: a.backstory ?? '',
    perms: {
      auto_yes: a.perms?.auto_yes ?? true,
      auto_commits: a.perms?.auto_commits ?? true,
      detect_urls: a.perms?.detect_urls ?? false,
    },
    flagsText: (a.flags ?? []).join(' '),
  })
}

function startCreate(): void {
  editingId.value = null
  Object.assign(form, emptyForm())
  showForm.value = true
}

function startEdit(a: Agent): void {
  editingId.value = a.id
  fillForm(a)
  showForm.value = true
}

function cancelForm(): void {
  showForm.value = false
  editingId.value = null
  Object.assign(form, emptyForm())
}

async function save(): Promise<void> {
  saving.value = true
  const payload: AgentInput = {
    ...form,
    flags: form.flagsText
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean),
  }
  try {
    const isEdit = !!editingId.value
    if (isEdit && editingId.value) await api.updateAgent(editingId.value, payload)
    else await api.createAgent(payload)
    cancelForm()
    push(isEdit ? 'агент обновлён' : 'агент создан')
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    saving.value = false
  }
}

async function toggle(a: Agent): Promise<void> {
  busy[a.id] = true
  try {
    if (a.status === 'running') await api.stopAgent(a.id)
    else await api.startAgent(a.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    busy[a.id] = false
  }
}

async function remove(a: Agent): Promise<void> {
  if (!confirm(`Удалить агента ${a.name}?`)) return
  try {
    await api.deleteAgent(a.id)
    push('агент удалён')
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}
</script>

<template>
  <section class="flex min-h-full flex-col">
    <div class="flex items-center justify-between gap-3 px-5 py-3">
      <h1 class="text-[13px] font-semibold uppercase tracking-widest text-vsc-muted">
        Агенты — aider-сессии
      </h1>
      <button class="btn-primary" @click="showForm && editingId === null ? cancelForm() : startCreate()">
        <VsIcon name="plus" :size="14" />
        {{ showForm && editingId === null ? 'Свернуть' : 'Новый агент' }}
      </button>
    </div>

    <!-- Editor-like form panel -->
    <form v-if="showForm" class="mx-5 mb-4 grid grid-cols-1 gap-3 border border-vsc-border bg-vsc-bg p-4 md:grid-cols-2" @submit.prevent="save">
      <div class="md:col-span-2 flex items-center justify-between border-b border-vsc-border pb-2">
        <h2 class="text-[13px] font-semibold">
          {{ editingId ? 'Редактирование агента' : 'Новый агент' }}
        </h2>
        <button v-if="editingId" type="button" class="btn-secondary !py-0.5 text-xs" @click="cancelForm">
          Отмена
        </button>
      </div>
      <div>
        <label class="label" for="f-name">Имя *</label>
        <input id="f-name" v-model="form.name" class="input" required placeholder="backend-dev" />
      </div>
      <div>
        <label class="label" for="f-workdir">Рабочая папка (абсолютный путь) *</label>
        <input id="f-workdir" v-model="form.workdir" class="input font-mono" required placeholder="D:/repos/my-project" />
      </div>
      <div>
        <label class="label" for="f-model">Модель aider (опционально)</label>
        <input id="f-model" v-model="form.model" class="input font-mono" placeholder="anthropic/claude-sonnet-4-5" />
      </div>
      <div>
        <label class="label" for="f-flags">Доп. флаги aider (через пробел/запятую)</label>
        <input id="f-flags" v-model="form.flagsText" class="input font-mono" placeholder="--no-gitignore --watch-files" />
      </div>
      <div>
        <label class="label" for="f-role">Роль в crew</label>
        <input id="f-role" v-model="form.role" class="input" placeholder="Senior backend engineer" />
      </div>
      <div>
        <label class="label" for="f-goal">Цель в crew</label>
        <input id="f-goal" v-model="form.goal" class="input" placeholder="Реализовать API-часть задачи" />
      </div>
      <div class="md:col-span-2">
        <label class="label" for="f-backstory">Backstory</label>
        <textarea id="f-backstory" v-model="form.backstory" class="input" rows="2" />
      </div>
      <fieldset class="md:col-span-2">
        <legend class="label">Права aider</legend>
        <div class="grid gap-1.5 text-[13px]">
          <label class="flex cursor-pointer items-center gap-2">
            <input v-model="form.perms.auto_yes" type="checkbox" class="checkbox" />
            <span>Авто-подтверждение <span class="text-vsc-muted">(--yes-always: aider молча соглашается на свои вопросы; выкл = каждый вопрос надо подтвердить вручную)</span></span>
          </label>
          <label class="flex cursor-pointer items-center gap-2">
            <input v-model="form.perms.auto_commits" type="checkbox" class="checkbox" />
            <span>Автокоммиты <span class="text-vsc-muted">(--auto-commits: aider сам делает git-коммиты)</span></span>
          </label>
          <label class="flex cursor-pointer items-center gap-2">
            <input v-model="form.perms.detect_urls" type="checkbox" class="checkbox" />
            <span>Переходить по ссылкам <span class="text-vsc-muted">(--detect-urls: может качать pandoc и ходить в веб; обычно не нужно)</span></span>
          </label>
          <p class="text-[11px] text-vsc-muted">
            Всегда включено: --no-check-update, --subtree-only (aider не выходит за пределы рабочей папки).
          </p>
        </div>
      </fieldset>
      <div class="md:col-span-2 flex gap-2">
        <button class="btn-primary" type="submit" :disabled="saving">
          {{ saving ? 'Сохранение…' : (editingId ? 'Сохранить изменения' : 'Создать') }}
        </button>
        <button class="btn-secondary" type="button" @click="cancelForm">Отмена</button>
      </div>
    </form>

    <!-- Loading skeleton -->
    <div v-if="loading" class="grid grid-cols-1 gap-3 px-5 md:grid-cols-2">
      <div v-for="i in 4" :key="i" class="h-24 animate-pulse border border-vsc-border bg-vsc-side" />
    </div>

    <div v-else-if="!agents.length" class="px-5 py-10 text-center text-sm text-vsc-muted">
      Агентов пока нет — создайте первого. Каждая сессия aider запускается в своей папке
      (рабочая директория задаётся процессу напрямую, без cd).
    </div>

    <!-- Agent rows like file list -->
    <div v-else class="px-5 pb-5">
      <div class="overflow-hidden border border-vsc-border">
        <div
          v-for="a in agents"
          :key="a.id"
          class="group flex items-center gap-3 border-b border-vsc-border bg-vsc-bg px-4 py-2.5 last:border-b-0 hover:bg-vsc-hover"
        >
          <button
            class="flex min-w-0 flex-1 items-center gap-3 text-left"
            @click="router.push({ name: 'agent', params: { id: a.id } })"
          >
            <VsIcon name="terminal" :size="16" class="text-vsc-muted" />
            <span class="min-w-0">
              <span class="block truncate text-[13px] font-medium text-vsc-text group-hover:text-vsc-active-text">
                {{ a.name }}
              </span>
              <span class="block truncate font-mono text-[11px] text-vsc-muted" :title="a.workdir">
                {{ a.workdir }}
              </span>
            </span>
            <span v-if="a.model" class="hidden shrink-0 font-mono text-[11px] text-vsc-cyan lg:inline">
              {{ a.model }}
            </span>
          </button>
          <StatusBadge :status="a.status" />
          <div class="flex shrink-0 items-center gap-1 opacity-60 transition-opacity group-hover:opacity-100">
            <button
              class="grid h-7 w-7 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text"
              :class="a.status === 'running' && 'text-vsc-green'"
              :disabled="busy[a.id]"
              :title="a.status === 'running' ? 'Остановить' : 'Запустить'"
              @click="toggle(a)"
            >
              <VsIcon :name="a.status === 'running' ? 'stop' : 'play'" :size="14" />
            </button>
            <button
              class="grid h-7 w-7 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text"
              :title="'Логи / чат'"
              @click="router.push({ name: 'agent', params: { id: a.id } })"
            >
              <VsIcon name="terminal" :size="14" />
            </button>
            <button
              class="grid h-7 w-7 place-items-center rounded text-vsc-muted hover:bg-vsc-btn2 hover:text-vsc-text disabled:cursor-not-allowed disabled:opacity-40"
              :title="'Изменить'"
              :disabled="a.status === 'running'"
              @click="startEdit(a)"
            >
              <VsIcon name="edit" :size="14" />
            </button>
            <button
              class="grid h-7 w-7 place-items-center rounded text-vsc-muted hover:bg-vsc-red hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
              :title="'Удалить'"
              :disabled="a.status === 'running'"
              @click="remove(a)"
            >
              <VsIcon name="trash" :size="14" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
