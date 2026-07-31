import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    component: () => import('src/layouts/MainLayout.vue'),
    children: [
      { path: '', name: 'home', component: () => import('src/pages/HomePage.vue') },
      { path: 'targets', name: 'targets', component: () => import('src/pages/TargetsPage.vue') },
      { path: 'generate', name: 'generate', component: () => import('src/pages/GeneratePage.vue') },
      { path: 'load', name: 'load', component: () => import('src/pages/LoadPage.vue') },
      { path: 'results', name: 'results', component: () => import('src/pages/ResultsPage.vue') },
      {
        path: 'results/:id',
        name: 'results-detail',
        component: () => import('src/pages/ResultsPage.vue'),
        props: true,
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
