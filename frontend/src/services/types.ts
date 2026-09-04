export interface AgentPerms {
  auto_yes: boolean
  auto_commits: boolean
  detect_urls: boolean
}

export interface Agent {
  id: string
  name: string
  workdir: string
  model?: string
  flags?: string[]
  env?: Record<string, string>
  role?: string
  goal?: string
  backstory?: string
  perms?: AgentPerms | null
  created_at: string
  status: 'running' | 'stopped'
}

export interface AgentInput {
  name: string
  workdir: string
  model: string
  flags: string[]
  env: Record<string, string>
  role: string
  goal: string
  backstory: string
  perms: AgentPerms
}

export type TaskStatus = 'pending' | 'running' | 'done' | 'failed' | 'canceled'

export type TaskMode = 'sequential' | 'parallel'

export interface Task {
  id: string
  title: string
  description: string
  agent_ids: string[]
  mode?: TaskMode
  workdir?: string
  shared_dir?: string
  base_dir?: string
  base_sha?: string
  status: TaskStatus
  result?: string
  error?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
}

export interface GitFile {
  path: string
  status: string
}

export interface GitStatus {
  repo: boolean
  branch?: string
  changes: GitFile[]
}

export interface TaskInput {
  title: string
  description: string
  agent_ids: string[]
  mode: TaskMode
  workdir: string
  shared_dir: string
}

export type EventKind = 'log' | 'thought' | 'status' | 'result' | 'error' | 'input' | 'plan'

export interface LogEvent {
  id: number
  ts: string
  source: 'agent' | 'crew' | 'system'
  ref: string
  agent?: string
  kind: EventKind
  text: string
}

export interface Health {
  status: string
  uptime_s: number
  agents: number
  tasks: number
  event_drops: number
}
