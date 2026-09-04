<script setup lang="ts">
import type { TaskStatus } from '../services/types'

const props = defineProps<{ status: TaskStatus | 'running' | 'stopped' }>()

const CLASSES: Record<string, string> = {
  running: 'bg-vsc-green',
  done: 'bg-vsc-green',
  stopped: 'bg-vsc-gray',
  pending: 'bg-vsc-yellow',
  failed: 'bg-vsc-red',
  canceled: 'bg-vsc-gray',
}
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-xs text-vsc-muted">
    <span
      class="h-2 w-2 rounded-full"
      :class="[CLASSES[props.status] ?? 'bg-vsc-gray', props.status === 'running' && 'animate-pulse']"
      aria-hidden="true"
    />
    {{ props.status }}
  </span>
</template>
