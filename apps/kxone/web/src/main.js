import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'vant/es/toast/style'
import 'vant/es/dialog/style'
import App from './App.vue'
import router from './router'
import './assets/styles/tokens.css'
import './assets/styles/main.css'
import './assets/styles/desktop.css'
import { installDialogAccessibility } from './utils/dialogAccessibility'

installDialogAccessibility()

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
app.mount('#app')
