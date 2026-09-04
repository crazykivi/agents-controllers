<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { api } from './services/api'
import { useTheme } from './composables/useTheme'
import { useToasts } from './composables/useToasts'
import { useAppStore } from './composables/useAppStore'
import { useEditorTabs } from './composables/useEditorTabs'
import { usePanelResize } from './composables/usePanelResize'
import { useApprovals } from './composables/useApprovals'
import VsIcon from './components/VsIcon.vue'

const { theme, cycle, label } = useTheme()
const { toasts } = useToasts()
const store = useAppStore()
const route = useRoute()
const router = useRouter()
const { pending: approvals, resolve: resolveApproval } = useApprovals()

const approvalsOpen = ref(true)
const resolving = ref<string | null>(null)

async function decide(id: string, action: 'allow' | 'deny'): Promise<void> {
  resolving.value = id
  try {
    await resolveApproval(id, action)
  } catch {
    /* панель обновится при следующем опросе */
  } finally {
    resolving.value = null
  }
}

const online = ref(true)
let ping: ReturnType<typeof setInterval> | null = null

async function check(): Promise<void> {
  try {
    await api.health()
    online.value = true
  } catch {
    online.value = false
  }
}

onMounted(() => {
  store.startPolling()
  void check()
  ping = setInterval(check, 30000)
})
onBeforeUnmount(() => {
  if (ping) clearInterval(ping)
  store.stopPolling()
  removeMenuListeners()
})

const sidebarView = ref<'agents' | 'tasks'>('agents')
const sidebarOpen = ref(localStorage.getItem('ac-sidebar-open') !== '0')
watch(sidebarOpen, (v) => {
  try {
    localStorage.setItem('ac-sidebar-open', v ? '1' : '0')
  } catch {
    // ignore
  }
})

const activity = computed<'agents' | 'tasks'>(() =>
  route.path.startsWith('/tasks') ? 'tasks' : 'agents',
)

function onActivity(view: 'agents' | 'tasks'): void {
  if (activity.value === view) {
    sidebarOpen.value = !sidebarOpen.value
    return
  }
  sidebarView.value = view
  sidebarOpen.value = true
  void router.push(view === 'agents' ? '/' : '/tasks')
}

// --- Resizable sidebar ---
const sidebarEl = ref<HTMLElement | null>(null)
const { size: sidebarWidth, dividerProps: sidebarSash } = usePanelResize('sidebar', sidebarEl, {
  min: 180,
  max: Math.max(340, Math.floor(window.innerWidth * 0.5)),
  initial: 240,
  side: 'start',
  axis: 'x',
})

// --- Editor tabs (VS Code-like) ---
const editorTabs = useEditorTabs()

const baseTabs = computed(() =>
  editorTabs.base.map((to) => ({
    to,
    title:
      to === '/'
        ? 'Агенты'
        : to === '/tasks'
          ? 'Задачи'
          : 'Настройки',
    icon: to === '/' ? 'agents' : to === '/tasks' ? 'tasks' : 'gear',
  })),
)

interface TabView {
  to: string
  title: string
  icon: string
  pinned: boolean
}

const sortedEditorTabs = computed<TabView[]>(() => {
  const views: TabView[] = editorTabs.tabs.map((to) => {
    const isAgent = to.startsWith('/agents/')
    const id = to.slice(to.lastIndexOf('/') + 1)
    return {
      to,
      title: isAgent ? store.agentName(id) : store.taskTitle(id),
      icon: isAgent ? 'terminal' : 'run',
      pinned: editorTabs.isPinned(to),
    }
  })
  const pinned = views.filter((t) => t.pinned)
  const rest = views.filter((t) => !t.pinned)
  return [...pinned, ...rest]
})

watch(
  () => route.fullPath,
  () => {
    if (route.name === 'agent' || route.name === 'task') editorTabs.open(route.path)
  },
  { immediate: true },
)

const activeTab = computed(() => route.path)

function closeTab(tab: TabView): void {
  if (tab.pinned) return
  const i = editorTabs.tabs.indexOf(tab.to)
  editorTabs.close(tab.to)
  if (route.path === tab.to) {
    const next = editorTabs.tabs[i] ?? editorTabs.tabs[i - 1]
    void router.push(next ?? '/')
  }
}

function closeOthersTab(tab: TabView): void {
  const removed = editorTabs.closeOthers(tab.to)
  if (removed.includes(route.path)) void router.push(tab.to)
}

function closeRightTab(tab: TabView): void {
  const removed = editorTabs.closeRight(tab.to)
  if (removed.includes(route.path)) {
    const i = editorTabs.tabs.indexOf(tab.to)
    const next = editorTabs.tabs[i + 1] ?? editorTabs.tabs[i - 1]
    void router.push(next ?? tab.to)
  }
}

function closeAllTabs(): void {
  const removed = editorTabs.closeAll()
  if (removed.includes(route.path)) void router.push('/')
}

// --- Tab context menu ---
const tabMenu = ref<{ x: number; y: number; tab: TabView } | null>(null)

// --- Tab drag & drop ---
const drag = ref<{ from: string; base: boolean } | null>(null)
const dropHint = ref<string | null>(null) // ключ вкладки, перед которой индикатор

function onDragStartBase(to: string, e: DragEvent): void {
  drag.value = { from: to, base: true }
  e.dataTransfer?.setData('text/plain', to)
  e.dataTransfer!.effectAllowed = 'move'
}
function onDragStart(tab: TabView, e: DragEvent): void {
  drag.value = { from: tab.to, base: false }
  e.dataTransfer?.setData('text/plain', tab.to)
  e.dataTransfer!.effectAllowed = 'move'
}
function onDragOverBase(e: DragEvent): void {
  if (!drag.value) return
  e.preventDefault()
  e.dataTransfer!.dropEffect = 'move'
}
function onDropBase(target: string, e: DragEvent): void {
  e.preventDefault()
  const d = drag.value
  drag.value = null
  dropHint.value = null
  if (!d) return
  // базовые вкладки меняются местами только между собой
  if (d.base && d.from !== target) editorTabs.swapBase(d.from, target)
}
function onDragOverTab(tab: TabView, e: DragEvent): void {
  if (!drag.value) return
  e.preventDefault()
  e.dataTransfer!.dropEffect = 'move'
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const before = e.clientX < r.left + r.width / 2
  dropHint.value = tab.to + (before ? ':l' : ':r')
}
function onDropTab(tab: TabView, e: DragEvent): void {
  e.preventDefault()
  const d = drag.value
  const hint = dropHint.value
  drag.value = null
  dropHint.value = null
  if (!d || !hint) return
  if (d.base) return // базовая вкладка падает только на другую базовую
  const before = hint.endsWith(':l')
  editorTabs.move(d.from, tab.to, before)
}
function onDragEnd(): void {
  drag.value = null
  dropHint.value = null
}

function hintClass(to: string): string {
  const h = dropHint.value
  if (!h) return ''
  if (h === to + ':l') return 'border-l-2 border-l-vsc-cyan'
  if (h === to + ':r') return 'border-r-2 border-r-vsc-cyan'
  return ''
}

function openTabMenu(tab: TabView, e: MouseEvent): void {
  const x = Math.min(e.clientX, window.innerWidth - 200)
  const y = Math.min(e.clientY, window.innerHeight - 210)
  tabMenu.value = { x: Math.max(4, x), y: Math.max(4, y), tab }
}

function runMenu(action: 'close' | 'others' | 'right' | 'all' | 'pin'): void {
  const tab = tabMenu.value?.tab
  tabMenu.value = null
  if (!tab) return
  if (action === 'close') closeTab(tab)
  else if (action === 'others') closeOthersTab(tab)
  else if (action === 'right') closeRightTab(tab)
  else if (action === 'all') closeAllTabs()
  else editorTabs.togglePin(tab.to)
}

function onWinClick(): void {
  tabMenu.value = null
}
function onWinKey(e: KeyboardEvent): void {
  if (e.key === 'Escape') tabMenu.value = null
}
function onWinScroll(): void {
  tabMenu.value = null
}
function onWinResize(): void {
  tabMenu.value = null
}

function addMenuListeners(): void {
  window.addEventListener('click', onWinClick)
  window.addEventListener('keydown', onWinKey)
  window.addEventListener('scroll', onWinScroll, true)
  window.addEventListener('resize', onWinResize)
}
function removeMenuListeners(): void {
  window.removeEventListener('click', onWinClick)
  window.removeEventListener('keydown', onWinKey)
  window.removeEventListener('scroll', onWinScroll, true)
  window.removeEventListener('resize', onWinResize)
}

watch(tabMenu, (m) => {
  if (m) addMenuListeners()
  else removeMenuListeners()
})
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-vsc-bg text-vsc-text">
    <!-- Title bar -->
    <header class="flex h-9 shrink-0 items-center bg-vsc-chrome px-3 select-none">
      <div class="flex items-center gap-2">
        <span class="grid h-5 w-5 place-items-center rounded bg-vsc-accent text-[11px] font-bold text-vsc-accent-text">A</span>
        <span class="text-[13px] font-medium">Agents Controllers</span>
      </div>
      <nav class="ml-6 flex items-center gap-4 text-[12px] text-vsc-muted" aria-label="Основная навигация">
        <RouterLink to="/" class="hover:text-vsc-text" active-class="text-vsc-text">Файл</RouterLink>
        <RouterLink to="/tasks" class="hover:text-vsc-text">Правка</RouterLink>
        <RouterLink to="/" class="hover:text-vsc-text">Вид</RouterLink>
      </nav>
      <div class="mx-auto -translate-x-8 text-[12px] text-vsc-muted">Agents Controllers</div>
      <div class="ml-auto flex items-center gap-1">
        <button
          class="grid h-7 w-7 place-items-center rounded text-vsc-text hover:bg-vsc-hover"
          :aria-label="`Тема: ${theme}`"
          @click="cycle"
        >
          <VsIcon :name="theme === 'light' ? 'sun' : 'moon'" :size="14" />
        </button>
      </div>
    </header>

    <div class="flex min-h-0 flex-1">
      <!-- Activity bar -->
      <nav
        class="flex w-12 shrink-0 flex-col items-center bg-vsc-activity py-1"
        aria-label="Activity bar"
      >
        <button
          class="relative grid h-12 w-12 place-items-center text-vsc-muted hover:text-vsc-text"
          :class="activity === 'agents' && 'text-vsc-active-text'"
          aria-label="Агенты"
          @click="onActivity('agents')"
        >
          <span
            v-if="activity === 'agents'"
            class="absolute left-0 top-1/2 h-6 w-0.5 -translate-y-1/2 bg-vsc-active-text"
          />
          <VsIcon name="agents" :size="22" />
        </button>
        <button
          class="relative grid h-12 w-12 place-items-center text-vsc-muted hover:text-vsc-text"
          :class="activity === 'tasks' && 'text-vsc-active-text'"
          aria-label="Задачи"
          @click="onActivity('tasks')"
        >
          <span
            v-if="activity === 'tasks'"
            class="absolute left-0 top-1/2 h-6 w-0.5 -translate-y-1/2 bg-vsc-active-text"
          />
          <VsIcon name="tasks" :size="22" />
        </button>
        <div class="mt-auto">
          <RouterLink
            to="/settings"
            class="grid h-12 w-12 place-items-center text-vsc-muted hover:text-vsc-text"
            :class="route.path === '/settings' && 'text-vsc-active-text'"
            aria-label="Настройки"
          >
            <VsIcon name="gear" :size="22" />
          </RouterLink>
        </div>
      </nav>

      <!-- Sidebar -->
      <aside
        v-show="sidebarOpen"
        ref="sidebarEl"
        class="flex shrink-0 flex-col overflow-hidden bg-vsc-side"
        :style="{ width: sidebarWidth + 'px' }"
      >
        <div class="flex h-9 shrink-0 items-center justify-between px-4">
          <span class="text-[11px] font-semibold uppercase tracking-widest text-vsc-muted">
            {{ sidebarView === 'agents' ? 'Агенты' : 'Задачи' }}
          </span>
          <button
            v-if="sidebarView === 'agents'"
            class="grid h-5 w-5 place-items-center rounded text-vsc-muted hover:bg-vsc-hover hover:text-vsc-text"
            aria-label="Новый агент"
            @click="router.push('/')"
          >
            <VsIcon name="plus" :size="14" />
          </button>
        </div>

        <div class="scroll-thin min-h-0 flex-1 overflow-y-auto pb-2">
          <!-- Agents tree -->
          <div v-if="sidebarView === 'agents'">
            <RouterLink
              v-for="a in store.agents.value"
              :key="a.id"
              :to="`/agents/${a.id}`"
              class="group flex h-[22px] items-center gap-2 px-4 text-[13px] hover:bg-vsc-hover"
              :class="route.params.id === a.id && 'bg-vsc-active text-vsc-active-text'"
              :title="a.workdir"
            >
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                :class="a.status === 'running' ? 'bg-vsc-green' : 'bg-vsc-gray'"
              />
              <span class="truncate">{{ a.name }}</span>
              <span class="ml-auto hidden text-[10px] text-vsc-muted group-hover:inline">{{ a.status }}</span>
            </RouterLink>
            <div v-if="store.loaded.value && !store.agents.value.length" class="px-4 py-2 text-xs text-vsc-muted">
              Агентов пока нет
            </div>
          </div>

          <!-- Tasks tree -->
          <div v-else>
            <RouterLink
              v-for="t in store.tasks.value"
              :key="t.id"
              :to="`/tasks/${t.id}`"
              class="group flex h-[22px] items-center gap-2 px-4 text-[13px] hover:bg-vsc-hover"
              :class="route.params.id === t.id && 'bg-vsc-active text-vsc-active-text'"
              :title="t.description"
            >
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                :class="{
                  'bg-vsc-green animate-pulse': t.status === 'running',
                  'bg-vsc-yellow': t.status === 'pending',
                  'bg-vsc-cyan animate-pulse': t.status === 'awaiting_approval',
                  'bg-vsc-red': t.status === 'failed',
                  'bg-vsc-gray': t.status === 'done' || t.status === 'canceled',
                }"
              />
              <span class="truncate">{{ t.title }}</span>
              <span v-if="t.mode === 'parallel'" class="text-[10px] text-vsc-cyan">∥</span>
            </RouterLink>
            <div v-if="store.loaded.value && !store.tasks.value.length" class="px-4 py-2 text-xs text-vsc-muted">
              Задач пока нет
            </div>
          </div>
        </div>
      </aside>

      <!-- Sidebar resize sash -->
      <div
        v-show="sidebarOpen"
        class="group relative z-10 w-1 shrink-0 cursor-col-resize touch-none select-none"
        v-bind="sidebarSash"
      >
        <div class="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-vsc-border group-hover:bg-vsc-cyan" />
      </div>

      <!-- Editor area -->
      <div class="flex min-w-0 flex-1 flex-col">
        <!-- Tabs -->
        <div class="scroll-thin flex h-9 shrink-0 items-stretch overflow-x-auto border-b border-vsc-border bg-vsc-chrome">
          <RouterLink
            v-for="b in baseTabs"
            :key="b.to"
            :to="b.to"
            draggable="true"
            class="flex shrink-0 items-center gap-2 border-r border-vsc-border px-4 text-[13px]"
            :class="
              [
                activeTab === b.to
                  ? 'border-t-2 border-t-vsc-cyan bg-vsc-bg text-vsc-active-text'
                  : 'bg-vsc-chrome text-vsc-muted hover:text-vsc-text',
                hintClass(b.to),
              ]
            "
            @dragstart="onDragStartBase(b.to, $event)"
            @dragover="onDragOverBase($event)"
            @drop="onDropBase(b.to, $event)"
            @dragend="onDragEnd"
          >
            <VsIcon :name="b.icon" :size="14" />
            <span>{{ b.title }}</span>
          </RouterLink>
          <RouterLink
            v-for="tab in sortedEditorTabs"
            :key="tab.to"
            :to="tab.to"
            draggable="true"
            class="group flex shrink-0 items-center gap-2 border-r border-vsc-border px-3 text-[13px] select-none"
            :class="
              [
                activeTab === tab.to
                  ? 'border-t-2 border-t-vsc-cyan bg-vsc-bg text-vsc-active-text'
                  : 'bg-vsc-chrome text-vsc-muted hover:text-vsc-text',
                hintClass(tab.to),
              ]
            "
            :title="tab.title"
            @contextmenu.prevent="openTabMenu(tab, $event)"
            @dblclick.prevent="editorTabs.togglePin(tab.to)"
            @dragstart="onDragStart(tab, $event)"
            @dragover="onDragOverTab(tab, $event)"
            @drop="onDropTab(tab, $event)"
            @dragend="onDragEnd"
          >
            <VsIcon :name="tab.icon" :size="14" />
            <span class="max-w-40 truncate">{{ tab.title }}</span>
            <button
              v-if="tab.pinned"
              class="grid h-4 w-4 place-items-center text-vsc-muted hover:text-vsc-text"
              :aria-label="`Открепить ${tab.title}`"
              title="Открепить"
              @click.prevent="editorTabs.togglePin(tab.to)"
            >
              <VsIcon name="pin" :size="11" />
            </button>
            <button
              v-else
              class="grid h-4 w-4 place-items-center rounded text-vsc-muted opacity-0 hover:bg-vsc-hover hover:text-vsc-text group-hover:opacity-100"
              :aria-label="`Закрыть ${tab.title}`"
              @click.prevent="closeTab(tab)"
            >
              <VsIcon name="close" :size="10" />
            </button>
          </RouterLink>
          <div class="ml-auto flex shrink-0 items-center pr-2">
            <button
              class="grid h-6 w-6 place-items-center rounded text-vsc-muted hover:bg-vsc-hover hover:text-vsc-text"
              aria-label="Обновить"
              @click="store.refresh()"
            >
              <VsIcon name="refresh" :size="14" />
            </button>
          </div>
        </div>

        <!-- Offline banner -->
        <div
          v-if="!online"
          role="alert"
          class="shrink-0 bg-vsc-red px-4 py-1.5 text-center text-xs text-white"
        >
          Нет связи с сервером — логи и управление могут быть недоступны.
        </div>

        <!-- Editor content -->
        <main class="scroll-thin min-h-0 flex-1 overflow-auto">
          <RouterView />
        </main>
      </div>
    </div>

    <!-- Approvals panel (IDE-style bottom notifications) -->
    <div
      v-if="approvals.length"
      class="shrink-0 border-t border-vsc-border bg-vsc-side"
    >
      <button
        class="flex w-full items-center gap-2 px-4 py-1 text-left text-[11px] font-semibold uppercase tracking-widest text-vsc-muted hover:bg-vsc-hover"
        @click="approvalsOpen = !approvalsOpen"
      >
        <VsIcon name="chevron" :size="11" :class="approvalsOpen ? '' : '-rotate-90'" />
        Требуют подтверждения ({{ approvals.length }})
      </button>
      <div v-if="approvalsOpen" class="max-h-44 overflow-y-auto scroll-thin">
        <div
          v-for="ap in approvals"
          :key="ap.id"
          class="flex items-center gap-3 border-t border-vsc-border px-4 py-2"
        >
          <VsIcon name="x" :size="13" class="shrink-0 text-vsc-yellow" />
          <RouterLink
            :to="`/agents/${ap.agent_id}`"
            class="shrink-0 font-mono text-[11px] text-vsc-cyan hover:underline"
          >
            {{ ap.agent_name }}
          </RouterLink>
          <span class="min-w-0 flex-1 truncate text-[12px] text-vsc-text" :title="ap.text">{{ ap.text }}</span>
          <button
            class="btn-primary !py-0.5 !px-3 text-xs"
            :disabled="resolving === ap.id"
            @click="decide(ap.id, 'allow')"
          >
            Разрешить
          </button>
          <button
            class="btn-secondary !py-0.5 !px-3 text-xs"
            :disabled="resolving === ap.id"
            @click="decide(ap.id, 'deny')"
          >
            Отклонить
          </button>
        </div>
      </div>
    </div>

    <!-- Status bar -->
    <footer class="flex h-6 shrink-0 items-center gap-4 bg-vsc-status px-3 text-[11px] text-vsc-accent-text">
      <button
        class="flex items-center gap-1.5 rounded px-1 hover:bg-vsc-status-hover"
        @click="router.push('/')"
      >
        <span
          class="h-2 w-2 rounded-full"
          :class="online ? 'bg-white' : 'animate-pulse bg-vsc-yellow'"
        />
        {{ online ? 'сервер ок' : 'нет связи' }}
      </button>
      <button class="flex items-center gap-1.5 rounded px-1 hover:bg-vsc-status-hover" @click="onActivity('agents')">
        <VsIcon name="agents" :size="12" />
        агенты: {{ store.runningAgents.value }}/{{ store.agents.value.length }}
      </button>
      <button class="flex items-center gap-1.5 rounded px-1 hover:bg-vsc-status-hover" @click="onActivity('tasks')">
        <VsIcon name="tasks" :size="12" />
        задачи: {{ store.activeTasks.value }}/{{ store.tasks.value.length }}
      </button>
      <button
        v-if="approvals.length"
        class="flex items-center gap-1.5 rounded bg-vsc-yellow px-1.5 font-semibold text-black hover:brightness-110"
        title="Есть вопросы, требующие решения"
        @click="approvalsOpen = !approvalsOpen"
      >
        <VsIcon name="x" :size="11" />
        {{ approvals.length }}
      </button>
      <button class="ml-auto rounded px-1 hover:bg-vsc-status-hover" @click="cycle">
        тема: {{ label() }}
      </button>
    </footer>

    <!-- Tab context menu -->
    <div
      v-if="tabMenu"
      class="fixed z-50 min-w-48 border border-vsc-border bg-vsc-side py-1 text-[13px] shadow-2xl"
      :style="{ left: tabMenu.x + 'px', top: tabMenu.y + 'px' }"
      role="menu"
    >
      <button
        class="block w-full px-3 py-1 text-left hover:bg-vsc-hover disabled:cursor-not-allowed disabled:opacity-40"
        :disabled="tabMenu.tab.pinned"
        role="menuitem"
        @click="runMenu('close')"
      >
        Закрыть
      </button>
      <button
        class="block w-full px-3 py-1 text-left hover:bg-vsc-hover"
        role="menuitem"
        @click="runMenu('others')"
      >
        Закрыть остальные
      </button>
      <button
        class="block w-full px-3 py-1 text-left hover:bg-vsc-hover"
        role="menuitem"
        @click="runMenu('right')"
      >
        Закрыть справа
      </button>
      <button
        class="block w-full px-3 py-1 text-left hover:bg-vsc-hover"
        role="menuitem"
        @click="runMenu('all')"
      >
        Закрыть все
      </button>
      <div class="my-1 border-t border-vsc-border" />
      <button
        class="block w-full px-3 py-1 text-left hover:bg-vsc-hover"
        role="menuitem"
        @click="runMenu('pin')"
      >
        {{ tabMenu.tab.pinned ? 'Открепить вкладку' : 'Закрепить вкладку' }}
      </button>
    </div>

    <!-- Toasts -->
    <div
      aria-live="polite"
      class="pointer-events-none fixed bottom-8 right-4 z-50 flex flex-col gap-2"
    >
      <div
        v-for="t in toasts"
        :key="t.id"
        class="flex items-center gap-2 px-4 py-2 text-xs text-white shadow-lg"
        :class="t.kind === 'error' ? 'bg-vsc-red' : 'bg-vsc-btn2 text-vsc-text'"
      >
        <VsIcon :name="t.kind === 'error' ? 'x' : 'check'" :size="12" />
        {{ t.text }}
      </div>
    </div>
  </div>
</template>
