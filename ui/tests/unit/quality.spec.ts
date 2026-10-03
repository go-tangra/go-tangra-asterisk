import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import QualityPanel from '../../src/views/calls/quality-panel.vue'
it('shows absent quality as unavailable',()=>{const panel=mount(QualityPanel);expect(panel.text()).toContain('Local media quality');expect(panel.text()).toContain('Peer media quality');expect(panel.text()).toContain('Unavailable');expect(panel.text()).not.toContain('GOOD')})
