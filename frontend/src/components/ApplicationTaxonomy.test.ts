import {mount,flushPromises} from "@vue/test-utils";
import {beforeEach,afterEach,expect,it,vi} from "vitest";
import ApplicationTaxonomy from "./ApplicationTaxonomy.vue";
import {configurationFixture,resetStores,response} from "../testSupport";
import {signedIn} from "../session";
beforeEach(()=>{resetStores();signedIn.value=true});afterEach(()=>{vi.unstubAllGlobals();vi.restoreAllMocks()});
it("saves sorted tags and restores only that leaf",async()=>{
 let cfg=configurationFixture({category:"tools",tags:["z"]},2,"openai/codex");
 const fetch=vi.fn(async (url:string,init?:RequestInit)=>{if(init?.method==="PATCH"){const body=JSON.parse(init.body as string);cfg={...cfg,revision:cfg.revision+1,effective:{...cfg.effective,...body.set},fields:{...cfg.fields,tags:{source:"custom",differs_from_template:true}}};return response(cfg)}return response(url.includes("taxonomy")?{items:[{id:"tools",kind:"categories",name:{en:"Tools","zh-CN":"工具"}},...['z','a'].map(id=>({id,kind:"tags",name:{en:id,"zh-CN":id}}))],total_pages:1}:cfg)});vi.stubGlobal("fetch",fetch);
 const wrapper=mount(ApplicationTaxonomy,{props:{application:"openai/codex"}});await flushPromises();await wrapper.findAll('input[type="checkbox"]')[1]!.setValue(true);await wrapper.get("form").trigger("submit");await flushPromises();let body=JSON.parse(fetch.mock.calls.filter(([,init])=>init?.method==="PATCH").at(-1)![1]!.body as string);expect(body).toEqual({revision:2,set:{tags:["a","z"]},unset:[]});
 await wrapper.findAll(".override-control button")[1]!.trigger("click");await wrapper.get("form").trigger("submit");await flushPromises();body=JSON.parse(fetch.mock.calls.filter(([,init])=>init?.method==="PATCH").at(-1)![1]!.body as string);expect(body).toEqual({revision:3,set:{},unset:["tags"]});wrapper.unmount();
});
