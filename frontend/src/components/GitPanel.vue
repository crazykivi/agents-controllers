<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { GitStatus } from '../services/types'
import { useToasts } from '../composables/useToasts'
import VsIcon from './VsIcon.vue'

const props = defineProps<{
  fetchStatus: () => Promise<GitStatus>
  fetchDiff: () => Promise<{ diff: string }>
  refreshKey?: number
}>()

const { push } = useToasts()

const status = ref<GitStatus | null>(null)
const diff = ref('')
const loading = ref(false)
const view = ref<'files' | 'diff'>('files')

const STATUS_CLASS: Record<string, string> = {
  M: 'text-vsc-yellow',
  A: 'text-vsc-green',
  '??': 'text-vsc-green',
  D: 'text-vsc-red',
  R: 'text-vsc-cyan',
  C: 'text-vsc-cyan',
}

function fileClass(s: string): string {
  const c = s.trim().charAt(0)
  return STATUS_CLASS[c] ?? 'text-vsc-muted'
}

const diffLines = computed(() =>
  diff.value.split('\n').map((line) => {
    let cls = 'text-vsc-text'
    if (line.startsWith('+')) cls = 'text-vsc-green'
    else if (line.startsWith('-')) cls = 'text-vsc-red'
    else if (line.startsWith('@@')) cls = 'text-vsc-cyan'
    else if (/^(diff |index |---|\+\+\+)/.test(line)) cls = 'text-vsc-muted'
    return { line, cls }
  }),
)

async function load(): Promise<void> {
  loading.value = true
  try {
    status.value = await props.fetchStatus()
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    loading.value = false
  }
}

async function loadDiff(): Promise<void> {
  try {
    diff.value = (await props.fetchDiff()).diff
  } catch (e) {
    diff.value = ''
    push((e as Error).message, 'error')
  }
}

function switchView(v: 'files' | 'diff'): void {
  view.value = v
  if (v === 'diff' && !diff.value) void loadDiff()
}

onMounted(load)
watch(() => props.refreshKey, load)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-1 border-b border-vsc-border px-3 py-1.5">
      <button
        class="px-2 py-0.5 text-[12px]"
        :class="view === 'files' ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="switchView('files')"
      >
        Изменения
      </button>
      <button
        class="px-2 py-0.5 text-[12px]"
        :class="view === 'diff' ? 'border-b-2 border-b-vsc-cyan text-vsc-active-text' : 'text-vsc-muted hover:text-vsc-text'"
        @click="switchView('diff')"
      >
        Дифф
      </button>
      <span v-if="status?.repo && status.branch" class="ml-2 flex items-center gap-1 text-[11px] text-vsc-muted">
        <VsIcon name="files" :size="11" />
        {{ status.branch }}
      </span>
      <button
        class="ml-auto grid h-5 w-5 place-items-center rounded text-vsc-muted hover:bg-vsc-hover hover:text-vsc-text"
        aria-label="Обновить"
        @click="view === 'diff' && diff ? void loadDiff() : load()"
      >
        <VsIcon name="refresh" :size="13" />
      </button>
    </div>

    <div class="scroll-thin min-h-0 flex-1 overflow-auto p-2 text-[12px]">
      <div v-if="loading" class="px-2 py-1 text-vsc-muted">загрузка…</div>

      <template v-else-if="view === 'files'">
        <div v-if="status && !status.repo" class="px-2 py-1 text-vsc-muted">
          Папка не является git-репозиторием.
        </div>
        <div v-else-if="status && !status.changes.length" class="px-2 py-1 text-vsc-muted">
          Изменений нет, рабочая копия чистая.
        </div>
        <div
          v-for="f in status?.changes ?? []"
          :key="f.path"
          class="flex items-center gap-2 px-2 py-0.5 font-mono hover:bg-vsc-hover"
          :title="`git status: ${f.status}`"
        >
          <span class="w-4 shrink-0 text-center" :class="fileClass(f.status)">{{ f.status.trim() || '?' }}</span>
          <span class="truncate">{{ f.path }}</span>
        </div>
      </template>

      <template v-else>
        <div v-if="!diff" class="px-2 py-1 text-vsc-muted">Диффа нет.</div>
        <pre v-else class="whitespace-pre-wrap break-words font-mono text-[11px] leading-5"><span v-for="(l, i) in diffLines" :key="i" :class="l.cls">{{ l.line }}
</span></pre>
      </template>
    </div>
  </div>
</template>
