import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { expect, it } from 'vitest'
import { useLatest } from '../../src/components/latest'
it('marks every earlier request stale when a newer one starts',()=>{const latest=useLatest();const first=latest.start(),second=latest.start();expect(first.aborted).toBe(true);expect(second.aborted).toBe(false)})
it('aborts the in-flight request when the view unmounts',()=>{let signal:AbortSignal|undefined;const view=mount(defineComponent({setup(){const latest=useLatest();signal=latest.start();return()=>h('div')}}));expect(signal?.aborted).toBe(false);view.unmount();expect(signal?.aborted).toBe(true)})
