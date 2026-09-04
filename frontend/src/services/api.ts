import type {
  Agent,
  AgentInput,
  Approval,
  GitStatus,
  Health,
  LogEvent,
  Rule,
  Task,
  TaskInput,
  TaskTemplate,
} from './types'

const BASE: string = import.meta.env.VITE_API_BASE ?? ''

async function req<T>(path: string, init: RequestInit = {}, timeoutMs = 15000): Promise<T> {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), timeoutMs)
  try {
    const res = await fetch(BASE + path, {
      ...init,
      signal: ctrl.signal,
      headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
    })
    if (!res.ok) {
      let msg = res.statusText
      try {
        const body = (await res.json()) as { error?: string }
        if (body.error) msg = body.error
      } catch {
        /* тело не JSON — оставляем statusText */
      }
      throw new Error(msg)
    }
    if (res.status === 204) return undefined as T
    return (await res.json()) as T
  } finally {
    clearTimeout(timer)
  }
}

export const eventsUrl = `${BASE}/api/events`

const arr = <T>(v: T[] | null | undefined): T[] => v ?? []

export const api = {
  health: () => req<Health>('/api/health', {}, 5000),

  listAgents: () => req<Agent[] | null>('/api/agents').then(arr),
  getAgent: (id: string) => req<Agent>(`/api/agents/${id}`),
  createAgent: (a: AgentInput) =>
    req<Agent>('/api/agents', { method: 'POST', body: JSON.stringify(a) }),
  updateAgent: (id: string, a: AgentInput) =>
    req<Agent>(`/api/agents/${id}`, { method: 'PUT', body: JSON.stringify(a) }),
  deleteAgent: (id: string) => req<{ ok: boolean }>(`/api/agents/${id}`, { method: 'DELETE' }),
  startAgent: (id: string) => req<Agent>(`/api/agents/${id}/start`, { method: 'POST' }),
  stopAgent: (id: string) => req<{ ok: boolean }>(`/api/agents/${id}/stop`, { method: 'POST' }),
  sendInput: (id: string, text: string) =>
    req<{ ok: boolean }>(`/api/agents/${id}/input`, { method: 'POST', body: JSON.stringify({ text }) }),
  agentLogs: (id: string, tail = 500) =>
    req<LogEvent[] | null>(`/api/agents/${id}/logs?tail=${tail}`).then(arr),
  agentGitStatus: (id: string) => req<GitStatus>(`/api/agents/${id}/git/status`),
  agentGitDiff: (id: string) => req<{ diff: string }>(`/api/agents/${id}/git/diff`),
  agentUndo: (id: string) => req<{ ok: boolean }>(`/api/agents/${id}/git/undo`, { method: 'POST' }),

  listTasks: () => req<Task[] | null>('/api/tasks').then(arr),
  getTask: (id: string) => req<Task>(`/api/tasks/${id}`),
  createTask: (t: TaskInput) =>
    req<Task>('/api/tasks', { method: 'POST', body: JSON.stringify(t) }),
  deleteTask: (id: string) => req<{ ok: boolean }>(`/api/tasks/${id}`, { method: 'DELETE' }),
  cancelTask: (id: string) => req<Task>(`/api/tasks/${id}/cancel`, { method: 'POST' }),
  approveTask: (id: string, approve: boolean) =>
    req<Task>(`/api/tasks/${id}/approve`, { method: 'POST', body: JSON.stringify({ approve }) }),
  restartTask: (id: string) => req<Task>(`/api/tasks/${id}/restart`, { method: 'POST' }),
  taskLogs: (id: string, tail = 1000) =>
    req<LogEvent[] | null>(`/api/tasks/${id}/logs?tail=${tail}`).then(arr),
  taskGitStatus: (id: string) => req<GitStatus>(`/api/tasks/${id}/git/status`),
  taskGitDiff: (id: string) => req<{ diff: string }>(`/api/tasks/${id}/git/diff`),
  taskRollback: (id: string) => req<Task>(`/api/tasks/${id}/git/rollback`, { method: 'POST' }),

  listApprovals: () => req<Approval[] | null>('/api/approvals').then(arr),
  resolveApproval: (id: string, action: 'allow' | 'deny') =>
    req<{ ok: boolean }>(`/api/approvals/${id}`, { method: 'POST', body: JSON.stringify({ action }) }),

  listRules: () => req<Rule[] | null>('/api/rules').then(arr),
  addRule: (pattern: string, action: 'allow' | 'deny') =>
    req<Rule>('/api/rules', { method: 'POST', body: JSON.stringify({ pattern, action }) }),
  deleteRule: (id: string) => req<{ ok: boolean }>(`/api/rules/${id}`, { method: 'DELETE' }),

  listTemplates: () => req<TaskTemplate[] | null>('/api/templates').then(arr),
  saveTemplate: (name: string, taskID: string) =>
    req<TaskTemplate>('/api/templates', {
      method: 'POST',
      body: JSON.stringify({ name, task_id: taskID }),
    }),
  createTemplateFromForm: (
    name: string,
    payload: {
      title: string
      description: string
      agent_ids: string[]
      mode: TaskInput['mode']
      workdir: string
      shared_dir: string
      confirm_plan: boolean
    },
  ) =>
    req<TaskTemplate>('/api/templates', {
      method: 'POST',
      body: JSON.stringify({ name, payload }),
    }),
  deleteTemplate: (id: string) => req<{ ok: boolean }>(`/api/templates/${id}`, { method: 'DELETE' }),
}
