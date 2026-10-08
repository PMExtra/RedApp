import {mount,flushPromises} from "@vue/test-utils";
import {beforeEach,afterEach,expect,it,vi} from "vitest";
import TaxonomyPage from "./TaxonomyPage.vue";
import {configurationFixture,resetStores,response} from "../testSupport";
import {signedIn} from "../session";
import {setLanguage} from "../i18n";
beforeEach(()=>{resetStores();signedIn.value=true});
afterEach(()=>{vi.unstubAllGlobals();vi.restoreAllMocks();setLanguage("en");document.body.innerHTML=""});
it("keeps same-value language overrides and preserves a conflict draft",async()=>{
 let revision=4,conflict=true;
 const item=()=>({...configurationFixture({name:{en:"Tools","zh-CN":"工具"}},revision,"categories:tools"),kind:"categories",id:"tools",name:{en:"Tools","zh-CN":"工具"},builtin:true});
 const fetch=vi.fn(async (_url:string,init?:RequestInit)=>{if(init?.method==="PATCH"){if(conflict){revision=5;return response({error:{code:"DIRECTORY_REVISION_CONFLICT"}},409)}revision++;return response(item())}return response({items:[item()],page:1,total:1,total_pages:1,limit:25})});vi.stubGlobal("fetch",fetch);
 const wrapper=mount(TaxonomyPage);await flushPromises();await wrapper.get(".directory-row button").trigger("click");
 await wrapper.findAll(".override-control button")[0]!.trigger("click");await wrapper.get("form.panel").trigger("submit");await flushPromises();
 expect((wrapper.findAll("form.panel input")[1]!.element as HTMLInputElement).value).toBe("Tools");expect(wrapper.find('[role="alert"]').text()).toContain("draft is preserved");
 vi.stubGlobal("confirm",vi.fn(()=>true));await wrapper.get('[aria-label="Reload"]').trigger("click");await flushPromises();await wrapper.findAll(".override-control button")[0]!.trigger("click");conflict=false;await wrapper.get("form.panel").trigger("submit");await flushPromises();const patches=fetch.mock.calls.filter(([,init])=>init?.method==="PATCH");expect(JSON.parse(patches[1]![1]!.body as string)).toEqual({revision:5,set:{"name.en":"Tools"},unset:[]});
 setLanguage("zh-CN");await flushPromises();expect(wrapper.text()).toContain("管理分类与标签");wrapper.unmount();
});
it("creates bilingual tags and requires delete confirmation",async()=>{
 let entries:unknown[]=[];const fetch=vi.fn(async (_url:string,init?:RequestInit)=>{if(init?.method==="POST"){const input=JSON.parse(init.body as string);const item={...configurationFixture({name:input.name},1),...input,kind:"tags",builtin:false};entries=[item];return response(item)}if(init?.method==="DELETE"){entries=[];return response({deleted:true})}return response({items:entries,page:1,total:entries.length,total_pages:1})});vi.stubGlobal("fetch",fetch);
 const wrapper=mount(TaxonomyPage);await flushPromises();await wrapper.findAll('[role="tab"]')[1]!.trigger("click");await flushPromises();await wrapper.get('[aria-label="Create"]').trigger("click");const fields=wrapper.findAll("form.panel input");await fields[0]!.setValue("cli");await fields[1]!.setValue("CLI");await fields[2]!.setValue("命令行");await wrapper.get("form.panel").trigger("submit");await flushPromises();expect(fetch.mock.calls.filter(([,init])=>init?.method==="POST")).toHaveLength(1);
 await wrapper.get('[aria-label="Delete"]').trigger("click");expect(fetch.mock.calls.filter(([,init])=>init?.method==="DELETE")).toHaveLength(0);expect(wrapper.find(".delete-review").text()).not.toContain("vendor");await wrapper.get(".delete-review .danger").trigger("click");await flushPromises();expect(fetch.mock.calls.filter(([,init])=>init?.method==="DELETE")).toHaveLength(1);wrapper.unmount();
});
