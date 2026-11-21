import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { useKeyboardViewport } from './useKeyboardViewport'

let wrapper
afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
})

describe('phone keyboard layout', () => {
  it('uses task controls in document flow only when an editor is focused and a keyboard shortens the viewport', async () => {
    const viewport = new EventTarget()
    Object.assign(viewport, { height: 812, scale: 1 })
    vi.stubGlobal('visualViewport', viewport)
    vi.stubGlobal('innerWidth', 375)
    vi.stubGlobal('innerHeight', 812)
    wrapper = mount({ setup: useKeyboardViewport, template: '<section><input aria-label="客户姓名"><span>{{ keyboardOpen }}</span></section>' }, { attachTo: document.body })
    viewport.height = 480
    viewport.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(wrapper.find('span').text()).toBe('false')
    wrapper.find('input').element.focus()
    await new Promise(resolve => queueMicrotask(resolve))
    await nextTick()
    expect(wrapper.find('span').text()).toBe('true')
    viewport.scale = 1.5
    viewport.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(wrapper.find('span').text()).toBe('false')
    viewport.scale = 1
    viewport.height = 812
    viewport.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(wrapper.find('span').text()).toBe('false')
  })
})
