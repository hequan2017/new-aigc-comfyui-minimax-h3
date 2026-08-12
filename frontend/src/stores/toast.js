import { defineStore } from 'pinia'

// 全局轻量通知：替代浏览器 alert，右上角堆叠，自动消失
export const useToastStore = defineStore('toast', {
  state: () => ({
    items: [],
    seq: 0
  }),
  actions: {
    show(message, type = 'info', timeout = 3400) {
      if (!message) return
      const id = ++this.seq
      this.items.push({ id, message, type })
      if (timeout > 0) {
        const seed = id
        setTimeout(() => this.dismiss(seed), timeout)
      }
      return id
    },
    success(msg, timeout) { return this.show(msg, 'success', timeout) },
    error(msg, timeout) { return this.show(msg, 'error', timeout ?? 5200) },
    info(msg, timeout) { return this.show(msg, 'info', timeout) },
    dismiss(id) {
      const i = this.items.findIndex((x) => x.id === id)
      if (i >= 0) this.items.splice(i, 1)
    }
  }
})
