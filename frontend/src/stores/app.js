import { defineStore } from 'pinia'

export const useAppStore = defineStore('app', {
  state: () => ({
    connected: false,
    instances: [],
    gpus: [],
    lastEvent: null,
    lastProjectUpdate: null
  }),
  actions: {
    setSnapshot(data) {
      if (data.instances) this.instances = data.instances
      if (data.gpus) this.gpus = data.gpus
    },
    setConnected(v) {
      this.connected = v
    }
  }
})
