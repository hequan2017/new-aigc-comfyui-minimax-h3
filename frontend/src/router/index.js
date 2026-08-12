import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/projects' },
  { path: '/dashboard', name: 'dashboard', component: () => import('../views/Dashboard.vue') },
  { path: '/create', name: 'create', component: () => import('../views/CreateTask.vue') },
  { path: '/tasks', name: 'tasks', component: () => import('../views/Tasks.vue') },
  { path: '/tasks/:id', name: 'task-detail', component: () => import('../views/TaskDetail.vue') },
  { path: '/instances', name: 'instances', component: () => import('../views/Instances.vue') },
  { path: '/projects', name: 'projects', component: () => import('../views/Projects.vue') },
  { path: '/projects/new', name: 'project-new', component: () => import('../views/ProjectNew.vue') },
  { path: '/projects/:id', name: 'project-detail', component: () => import('../views/ProjectDetail.vue') },
  { path: '/projects/:id/editor', name: 'project-editor', component: () => import('../views/ProjectEditor.vue') },
  { path: '/skills', name: 'skills', component: () => import('../views/Skills.vue') },
  { path: '/materials', name: 'materials', component: () => import('../views/Materials.vue') },
  { path: '/settings', name: 'settings', component: () => import('../views/Settings.vue') }
]

export default createRouter({
  history: createWebHistory(),
  routes
})
