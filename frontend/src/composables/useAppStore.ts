import { computed, ref } from 'vue'
import type { Agent, Task } from '../services/types'
import { api } from '../services/api'

const agents = ref<Agent[]>([])
const tasks = ref<Task[]>([])
const loaded = ref(false)

const runningAgents = computed(() => agents.value.filter((a) => a.status === 'running').length)
const activeTasks = computed(
  () =>
    tasks.value.filter(
      (t) =>
        t.status === 'running' || t.status === 'pending' || t.status === 'awaiting_approval',
    ).length,
)

function agentName(id: string): string {
  return agents.value.find((a) => a.id === id)?.name ?? id
}

function taskTitle(id: string): string {
  return tasks.value.find((t) => t.id === id)?.title ?? id
}

async function refresh(): Promise<void> {
  try {
    const [a, t] = await Promise.all([api.listAgents(), api.listTasks()])
    agents.value = a
    tasks.value = t
    loaded.value = true
  } catch {
    /* доступность сервера показывается отдельно */
  }
}

let poll: ReturnType<typeof setInterval> | null = null

function startPolling(): void {
  if (poll) return
  void refresh()
  poll = setInterval(() => void refresh(), 5000)
}

function stopPolling(): void {
  if (poll) {
    clearInterval(poll)
    poll = null
  }
}

export function useAppStore(): {
  agents: typeof agents
  tasks: typeof tasks
  loaded: typeof loaded
  runningAgents: typeof runningAgents
  activeTasks: typeof activeTasks
  refresh: typeof refresh
  agentName: typeof agentName
  taskTitle: typeof taskTitle
  startPolling: typeof startPolling
  stopPolling: typeof stopPolling
} {
  return { agents, tasks, loaded, runningAgents, activeTasks, refresh, agentName, taskTitle, startPolling, stopPolling }
}
