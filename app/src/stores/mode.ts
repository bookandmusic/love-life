import { ref } from 'vue'
import { defineStore } from 'pinia'
import { KEYS } from '../utils/storage'

export type AppMode = 'traditional' | 'ai'
const KEY = KEYS.MODE

export const useMode = defineStore('mode', () => {
  const mode = ref<AppMode>(((localStorage.getItem(KEY) as AppMode) || 'traditional'))

  function setMode(m: AppMode) {
    mode.value = m
    localStorage.setItem(KEY, m)
  }

  return { mode, setMode }
})
