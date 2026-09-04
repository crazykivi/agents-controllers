import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: () => import('../views/DashboardView.vue') },
    {
      path: '/agents/:id',
      name: 'agent',
      component: () => import('../views/AgentDetailView.vue'),
      props: true,
    },
    { path: '/tasks', name: 'tasks', component: () => import('../views/TasksView.vue') },
    {
      path: '/tasks/:id',
      name: 'task',
      component: () => import('../views/TaskDetailView.vue'),
      props: true,
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

export default router
