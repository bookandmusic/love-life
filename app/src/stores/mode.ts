import { ref } from 'vue'
import { defineStore } from 'pinia'
import { KEYS, getItem, setItem } from '../utils/storage'

export type AppMode = 'traditional' | 'ai'
const KEY = KEYS.MODE

export const useMode = defineStore('mode', () => {
  const mode = ref<AppMode>(((getItem(KEY) as AppMode) || 'traditional'))

  function setMode(m: AppMode) {
    mode.value = m
    setItem(KEY, m)
  }

  return { mode, setMode }
})
