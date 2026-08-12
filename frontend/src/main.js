import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './styles/main.css'

// 早设主题，避免首屏闪烁
document.documentElement.setAttribute('data-theme', localStorage.getItem('theme') || 'light')

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
