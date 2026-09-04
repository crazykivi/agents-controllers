export function fmtTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function fmtDuration(startIso?: string | null, endIso?: string | null): string {
  if (!startIso) return '—'
  const start = new Date(startIso).getTime()
  const end = endIso ? new Date(endIso).getTime() : Date.now()
  const s = Math.max(0, Math.round((end - start) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

export function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n) + '…'
}

const KIND_CLASSES: Record<string, string> = {
  log: 'text-slate-300',
  thought: 'text-violet-300',
  status: 'text-sky-300',
  result: 'text-emerald-300',
  error: 'text-rose-400',
  input: 'text-amber-300',
  plan: 'text-cyan-300',
}

export function kindClass(kind: string): string {
  return KIND_CLASSES[kind] ?? 'text-slate-300'
}

export function stripAnsi(s: string): string {
  return s.replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, '')
}
