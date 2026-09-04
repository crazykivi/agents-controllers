<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { LogEvent } from '../services/types'
import { fmtTime, kindClass, stripAnsi } from '../utils/format'

const props = defineProps<{ events: LogEvent[] }>()

const MAX_RENDER = 800
const el = ref<HTMLElement | null>(null)
const autoscroll = ref(true)

const shown = computed(() => props.events.slice(-MAX_RENDER))

watch(
  () => props.events.length,
  async () => {
    if (!autoscroll.value) return
    await nextTick()
    el.value?.scrollTo({ top: el.value.scrollHeight })
  },
)
</script>

<template>
  <div class="scroll-thin flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-vsc-term">
    <div
      ref="el"
      role="log"
      aria-live="polite"
      class="scroll-thin flex-1 overflow-auto px-4 py-2 font-mono text-xs leading-[1.55]"
    >
      <div v-for="e in shown" :key="e.id" class="whitespace-pre-wrap break-words">
        <span class="text-vsc-muted">{{ fmtTime(e.ts) }}</span>
        <span v-if="e.agent" class="text-vsc-muted"> [{{ e.agent }}]</span>
        <span :class="kindClass(e.kind)">{{ stripAnsi(e.text) }}</span>
      </div>
      <div v-if="!shown.length" class="text-vsc-muted">нет вывода…</div>
    </div>
    <label class="flex shrink-0 items-center gap-1.5 border-t border-vsc-border px-4 py-1 text-[11px] text-vsc-muted">
      <input v-model="autoscroll" type="checkbox" class="checkbox" />
      автоскролл
    </label>
  </div>
</template>
