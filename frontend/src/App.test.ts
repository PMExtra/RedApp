import {mount,flushPromises} from '@vue/test-utils'
import {afterEach,describe,it,expect,vi} from 'vitest'
import App from './App.vue'
import Maintenance from './components/Maintenance.vue'
const status={name:'RedApp',public_base_url:'https://redapp.example',sampled_at:'now',os:'linux',arch:'amd64',go:'go1.27.1',goroutines:4,memory_bytes:1024,disk:{cache_bytes:0,temporary_bytes:0,pending_bytes:0,used_bytes:1024,free_bytes:10000},rates:{upstream_bytes_per_second:0,downstream_bytes_per_second:0},resources:[],versions:{},events:[],counters:{}}
const response=(data:unknown,status=200)=>({ok:status===200,status,json:async()=>data})
afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals()})
describe('Admin session and requests',()=>{
 it('loads the existing status API, recovers errors, expires session, and cancels polling',async()=>{
  vi.useFakeTimers();let phase='ready';const fetch=vi.fn(async(url:string,_options?:RequestInit)=>url.endsWith('/session')?response({csrf:'test-token'}):phase==='error'?response({error:'Storage unavailable'},503):phase==='expired'?response({error:'Sign in required'},401):response(status));vi.stubGlobal('fetch',fetch)
  const wrapper=mount(App);await flushPromises();expect(wrapper.text()).toContain('Distribution overview');phase='error';await wrapper.findAll('button').find(button=>button.text()==='Refresh')!.trigger('click');await flushPromises();expect(wrapper.text()).toContain('Storage unavailable');phase='ready';await wrapper.findAll('button').find(button=>button.text()==='Retry')!.trigger('click');await flushPromises();expect(wrapper.text()).not.toContain('Storage unavailable');phase='expired';await vi.advanceTimersByTimeAsync(5000);await flushPromises();expect(wrapper.text()).toContain('Your session expired');const calls=fetch.mock.calls.length;await vi.advanceTimersByTimeAsync(10000);expect(fetch.mock.calls.length).toBe(calls)
  wrapper.unmount()
 })
 it('logs in and sends CSRF, then signs out',async()=>{
  const fetch=vi.fn(async(url:string,_options?:RequestInit)=>url.endsWith('/session')?response({error:'Sign in required'},401):url.endsWith('/login')?response({csrf:'login-token'}):url.endsWith('/logout')?response({ok:true}):response(status));vi.stubGlobal('fetch',fetch)
  const wrapper=mount(App);await flushPromises();await wrapper.find('input[type=password]').setValue('dummy-password');await wrapper.find('form').trigger('submit');await flushPromises();expect(wrapper.text()).toContain('Distribution overview')
  const logout=wrapper.findAll('button').find(button=>button.text()==='Sign out')!;await logout.trigger('click');await flushPromises();expect(wrapper.text()).toContain('Signed out');const options=fetch.mock.calls.find(call=>call[0].endsWith('/logout'))?.[1] as RequestInit;expect((options.headers as Record<string,string>)['X-CSRF-Token']).toBe('login-token');wrapper.unmount()
 })
 it('requires preview before cleanup and invalidates stale preview on edit',async()=>{
  const fetch=vi.fn(async(url:string,_options?:RequestInit)=>url.endsWith('/preview')?response({job:{ID:'job',Selected:[{Resource:'r',Generation:'g'}]},logical_bytes:100,active:1,unknown_versions:[]}):response({ok:true}));vi.stubGlobal('fetch',fetch)
  const wrapper=mount(Maintenance);expect(wrapper.text()).not.toContain('Confirm this preview');await wrapper.find('input[placeholder="0.150.0"]').setValue('0.159.2');await wrapper.find('form.cleanup').trigger('submit');await flushPromises();expect(wrapper.text()).toContain('Confirm this preview');await wrapper.find('input[placeholder="0.150.0"]').setValue('0.160.0');expect(wrapper.text()).not.toContain('Confirm this preview');wrapper.unmount()
 })
})
