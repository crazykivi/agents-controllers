import { ref, watchEffect } from 'vue'

type Theme = 'light' | 'dark' | 'auto'

const STORAGE_KEY = 'ac-theme'
const mq = window.matchMedia('(prefers-color-scheme: dark)')

const theme = ref<Theme>((localStorage.getItem(STORAGE_KEY) as Theme) || 'dark')

function apply(): void {
  const light = theme.value === 'light' || (theme.value === 'auto' && !mq.matches)
  document.documentElement.classList.toggle('light', light)
}

watchEffect(() => {
  localStorage.setItem(STORAGE_KEY, theme.value)
  apply()
})
mq.addEventListener('change', apply)

const ORDER: Theme[] = ['dark', 'light', 'auto']

export function useTheme(): { theme: typeof theme; cycle: () => void; label: () => string } {
  const cycle = (): void => {
    theme.value = ORDER[(ORDER.indexOf(theme.value) + 1) % ORDER.length]
  }
  const label = (): string => (theme.value === 'auto' ? 'auto' : theme.value)
  return { theme, cycle, label }
}
