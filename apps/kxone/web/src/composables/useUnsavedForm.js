import { computed, h, onBeforeUnmount, ref, unref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { showConfirmDialog, showFailToast } from 'vant'

// Keep drafts in memory only: contact details must not persist on shared devices.
export const useUnsavedForm = (form, submitting) => {
  const snapshot = () => JSON.stringify(unref(form))
  const saved = ref(snapshot())
  const dirty = computed(() => snapshot() !== saved.value)
  const markSaved = () => { saved.value = snapshot() }
  const confirmLeave = async () => {
    if (unref(submitting)) {
      showFailToast('正在提交，请稍候')
      return false
    }
    if (!dirty.value) return true
    try {
      const title = '离开当前表单？'
      await showConfirmDialog({
        title,
        // This Vant version uses title itself as aria-labelledby. Supply its
        // target without leaving custom attributes on the shared dialog instance.
        message: () => h('span', [h('span', { id: title, class: 'sr-only' }, title), '填写内容尚未提交，离开后将丢失。']),
        confirmButtonText: '放弃填写', cancelButtonText: '继续填写'
      })
      return true
    } catch { return false }
  }
  onBeforeRouteLeave(confirmLeave)
  onBeforeRouteUpdate((to, from) => to.params.id !== from.params.id ? confirmLeave() : true)
  const beforeUnload = event => {
    if (!dirty.value && !unref(submitting)) return
    event.preventDefault()
    event.returnValue = ''
  }
  window.addEventListener('beforeunload', beforeUnload)
  onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
  return { markSaved }
}
