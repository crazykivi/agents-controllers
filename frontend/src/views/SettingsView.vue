<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api } from '../services/api'
import type { Rule } from '../services/types'
import { useToasts } from '../composables/useToasts'
import { useAppStore } from '../composables/useAppStore'
import VsIcon from '../components/VsIcon.vue'

const { push } = useToasts()
const store = useAppStore()

const rules = ref<Rule[]>([])
const saving = ref(false)
const form = reactive({ pattern: '', action: 'deny' as 'allow' | 'deny' })

async function load(): Promise<void> {
  try {
    rules.value = await api.listRules()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}
onMounted(() => {
  void load()
  void store.refresh()
})

async function add(): Promise<void> {
  if (!form.pattern.trim()) return
  saving.value = true
  try {
    await api.addRule(form.pattern.trim(), form.action)
    form.pattern = ''
    await load()
    push('правило добавлено')
  } catch (e) {
    push((e as Error).message, 'error')
  } finally {
    saving.value = false
  }
}

async function remove(r: Rule): Promise<void> {
  try {
    await api.deleteRule(r.id)
    await load()
  } catch (e) {
    push((e as Error).message, 'error')
  }
}
</script>

<template>
  <section class="flex min-h-full flex-col">
    <div class="px-5 py-3">
      <h1 class="text-[13px] font-semibold uppercase tracking-widest text-vsc-muted">Настройки</h1>
    </div>

    <div class="mx-5 mb-6 max-w-3xl">
      <!-- Agent perms overview -->
      <h2 class="mb-2 text-sm font-semibold">Права агентов</h2>
      <p class="mb-3 text-xs text-vsc-muted">
        Базовые права задаются у каждого агента в форме (кнопка «Изменить» на странице агентов):
        авто-подтверждение вопросов, автокоммиты, переход по ссылкам. Ниже — правила
        авто-ответов, которые применяются ко всем агентам, когда авто-подтверждение выключено:
        если текст вопроса содержит паттерн, ответ отправляется без человека.
      </p>

      <!-- Rules -->
      <div class="mb-6 border border-vsc-border">
        <div class="panel-title border-b border-vsc-border">Правила авто-ответов</div>
        <form class="flex items-end gap-2 border-b border-vsc-border p-3" @submit.prevent="add">
          <div class="min-w-0 flex-1">
            <label class="label" for="r-pattern">Паттерн (подстрока текста вопроса)</label>
            <input
              id="r-pattern"
              v-model="form.pattern"
              class="input font-mono"
              placeholder="например: install pandoc"
            />
          </div>
          <div>
            <label class="label" for="r-action">Действие</label>
            <select id="r-action" v-model="form.action" class="input w-32">
              <option value="allow">разрешать</option>
              <option value="deny">отклонять</option>
            </select>
          </div>
          <button class="btn-primary shrink-0" type="submit" :disabled="saving || !form.pattern.trim()">
            <VsIcon name="plus" :size="13" />
            Добавить
          </button>
        </form>
        <div v-if="!rules.length" class="px-4 py-3 text-xs text-vsc-muted">
          Правил нет — все вопросы при выключенном авто-подтверждении ждут решения вручную.
        </div>
        <div
          v-for="r in rules"
          :key="r.id"
          class="group flex items-center gap-3 border-b border-vsc-border px-4 py-2 last:border-b-0"
        >
          <span
            class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase"
            :class="r.action === 'allow' ? 'bg-vsc-green-dim text-vsc-green' : 'bg-vsc-red-dim text-vsc-red'"
          >
            {{ r.action === 'allow' ? 'разрешено' : 'запрещено' }}
          </span>
          <span class="min-w-0 flex-1 truncate font-mono text-[12px]" :title="r.pattern">{{ r.pattern }}</span>
          <button
            class="grid h-6 w-6 shrink-0 place-items-center rounded text-vsc-muted opacity-60 hover:bg-vsc-red hover:text-white group-hover:opacity-100"
            aria-label="Удалить правило"
            @click="remove(r)"
          >
            <VsIcon name="trash" :size="13" />
          </button>
        </div>
      </div>

      <!-- Agents quick perms table -->
      <h2 class="mb-2 text-sm font-semibold">Текущие агенты</h2>
      <div class="border border-vsc-border">
        <div
          v-for="a in store.agents.value"
          :key="a.id"
          class="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-vsc-border px-4 py-2 text-[12px] last:border-b-0"
        >
          <span class="w-40 truncate font-medium">{{ a.name }}</span>
          <span class="text-vsc-muted">авто-yes: {{ a.perms?.auto_yes ? 'вкл' : 'выкл' }}</span>
          <span class="text-vsc-muted">автокоммиты: {{ a.perms?.auto_commits ? 'вкл' : 'выкл' }}</span>
          <span class="text-vsc-muted">ссылки: {{ a.perms?.detect_urls ? 'вкл' : 'выкл' }}</span>
        </div>
        <div v-if="!store.agents.value.length" class="px-4 py-3 text-xs text-vsc-muted">Агентов пока нет.</div>
      </div>
    </div>
  </section>
</template>
