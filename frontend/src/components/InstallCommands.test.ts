import {mount} from '@vue/test-utils'
import {describe,it,expect,vi} from 'vitest'
import InstallCommands from './InstallCommands.vue'
describe('Install commands',()=>{
 it('renders separate parameter-free commands and copies exact text',async()=>{
  const writeText=vi.fn().mockResolvedValue(undefined);Object.defineProperty(navigator,'clipboard',{value:{writeText},configurable:true})
  const wrapper=mount(InstallCommands,{props:{origin:'https://redapp.example:8443'}})
  expect(wrapper.findAll('code').map(item=>item.text())).toEqual(["curl -fsSL 'https://redapp.example:8443/install.sh' | sh","irm 'https://redapp.example:8443/install.ps1' | iex"])
  for(const button of wrapper.findAll('button'))await button.trigger('click')
  expect(writeText.mock.calls.map(call=>call[0])).toEqual(wrapper.findAll('code').map(item=>item.text()))
  expect(wrapper.text()).toContain('Command copied')
  await wrapper.setProps({origin:'https://other.example'});expect(wrapper.find('code').text()).toContain('https://other.example/')
 })
 it('shows clipboard errors without injecting HTML',async()=>{
  Object.defineProperty(navigator,'clipboard',{value:{writeText:vi.fn().mockRejectedValue(Error('Permission denied'))},configurable:true})
  const wrapper=mount(InstallCommands,{props:{origin:'<img src=x onerror=alert(1)>'}})
  await wrapper.find('button').trigger('click');await vi.waitFor(()=>expect(wrapper.text()).toContain('Permission denied'));expect(wrapper.find('img').exists()).toBe(false)
 })
})
