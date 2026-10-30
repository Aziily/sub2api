import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import ResourceGroupsView from '../ResourceGroupsView.vue'
const mocks = vi.hoisted(() => ({ route: {path:'/admin/groups/storage'}, get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('vue-router', () => ({useRoute: () => mocks.route}))
vi.mock('vue-i18n', () => ({useI18n: () => ({t: (key: string) => key})}))
vi.mock('@/api/client', () => ({default: {get:mocks.get,post:mocks.post,put:mocks.put}}))
vi.mock('@/components/layout/AppLayout.vue', () => ({default:{template:'<div><slot /></div>'}}))
vi.mock('@/components/common/BaseDialog.vue', () => ({default:{props:['show'],template:'<div v-if="show"><slot /><slot name="footer" /></div>'}}))
const storage = {groups:[{id:1,kind:'storage',name:'Storage team',description:'',version:2,archived:false,account_ids:[1]}],accounts:[{id:1,email:'storage@example.com',enabled:true},{id:2,email:'second@example.com',enabled:false}]}
const mountView = () => mount(ResourceGroupsView, {global:{stubs:{RouterLink:{template:'<a><slot /></a>'}}}})
beforeEach(() => {vi.resetAllMocks();mocks.route=reactive({path:'/admin/groups/storage'});mocks.get.mockResolvedValue({data:storage});mocks.put.mockResolvedValue({data:{}})})
describe('Resource group management', () => {
 it('edits account membership with its version and preserves the form on conflict', async () => {
  const w=mountView();await flushPromises()
  expect(w.text()).toContain('Storage team')
  await w.findAll('button').find(b=>b.text()==='compute.editGroup')!.trigger('click')
  const checkboxes=w.findAll('fieldset input[type="checkbox"]')
  expect(checkboxes[0].element.checked).toBe(true)
  await checkboxes[1].setValue(true)
  mocks.put.mockRejectedValueOnce({status:409,message:'Refresh and retry'})
  await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.put).toHaveBeenCalledWith('/admin/resource-groups/storage/1', expect.objectContaining({version:2,account_ids:[1,2]}))
  expect(w.get('form').text()).toContain('Refresh and retry')
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.find('form').exists()).toBe(false)
  w.unmount()
 })
 it('does not show a stale storage response after switching to compute groups', async () => {
  let resolveStorage!: (v: unknown)=>void
  mocks.get.mockImplementationOnce(()=>new Promise(resolve=>{resolveStorage=resolve}))
  const w=mountView()
  mocks.get.mockResolvedValueOnce({data:{groups:[],accounts:[{id:9,email:'compute@example.com',enabled:true}]}})
  mocks.route.path='/admin/groups/compute';await flushPromises()
  resolveStorage({data:storage});await flushPromises()
  expect(mocks.get).toHaveBeenLastCalledWith('/admin/resource-groups/compute')
  expect(w.text()).not.toContain('Storage team')
  await w.findAll('button').find(b=>b.text()==='compute.createGroup')!.trigger('click')
  expect(w.get('form').text()).toContain('compute@example.com')
  expect(w.get('form').text()).not.toContain('storage@example.com')
  w.unmount()
 })
})
