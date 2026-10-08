import {mount,flushPromises} from "@vue/test-utils";
import {afterEach,expect,it,vi} from "vitest";
import PrewarmSettings from "./PrewarmSettings.vue";
import {configurationFixture,response,resetStores} from "../testSupport";
import {setLanguage} from "../i18n";
afterEach(()=>{vi.unstubAllGlobals();setLanguage("en");localStorage.clear()});
const limits={max_files:10000,max_depth:16,max_download_bytes:10737418240,max_duration_seconds:3600};
it("starts selected platforms and writes a sparse automatic policy",async()=>{
 resetStores();setLanguage("en");
 const fetch=vi.fn(async(url:string,init?:RequestInit)=>{
  if(url.endsWith("/configuration"))return response(configurationFixture({prewarm:{enabled:false,channels:[],platforms:[]}},3,"openai/codex"));
  if(url.endsWith("/options"))return response({release:true,platforms:[{id:"linux-x64",name:"linux-x64"}],channels:["latest"],limits});
  if(url.endsWith("/start"))return response({id:"a".repeat(32),state:"completed",completed:1,succeeded:1,bytes:4});
  if(url.includes("/items"))return response({items:[{key:"binary",status:"downloaded",bytes:4}],total:1,total_pages:1});return response({});
 });vi.stubGlobal("fetch",fetch);
 const w=mount(PrewarmSettings,{props:{application:"openai/codex"}});await flushPromises();
 await w.findAll("input[type=checkbox]")[0]!.setValue(true);await w.findAll("form")[0]!.trigger("submit");await flushPromises();
 const start=fetch.mock.calls.find(([url])=>url.endsWith("/start"))!;
 expect(JSON.parse(start[1]!.body as string)).toMatchObject({target:"latest",platforms:["linux-x64"],limits});
 const auto=w.findAll("form")[1]!;for(const input of auto.findAll("input[type=checkbox]"))await input.setValue(true);
 await auto.trigger("submit");await flushPromises();
 const patch=fetch.mock.calls.find(([,init])=>init?.method==="PATCH")!;
 expect(JSON.parse(patch[1]!.body as string)).toEqual({revision:3,set:{prewarm:{enabled:true,channels:["latest"],platforms:["linux-x64"]}},unset:[]});
 expect(w.text()).toContain("Task read limit (bytes)");w.unmount();
});
it("sends HTTP paths and RE2 filter, and clears inputs on app change",async()=>{
 resetStores();setLanguage("zh-CN");
 const fetch=vi.fn(async(url:string,init?:RequestInit)=>{
  if(url.endsWith("/configuration"))return response(configurationFixture({},1,"vendor/http"));
  if(url.endsWith("/options"))return response({release:false,platforms:[],channels:[],limits});
  if(url.endsWith("/start"))return response({id:"a".repeat(32),state:"limited",completed:0,succeeded:0,bytes:0});
  if(url.includes("/items"))return response({items:[],total:0,total_pages:1});return response({});
 });vi.stubGlobal("fetch",fetch);
 const w=mount(PrewarmSettings,{props:{application:"vendor/http"}});await flushPromises();
 await w.findAll("textarea")[0]!.setValue("/one\n/two");await w.findAll("textarea")[1]!.setValue("/");await w.get("select").setValue("re2");await w.get("input:not([type])").setValue("/.*\\.tar");await w.get("form").trigger("submit");await flushPromises();
 const start=fetch.mock.calls.find(([url])=>url.endsWith("/start"))!;
 expect(JSON.parse(start[1]!.body as string)).toMatchObject({paths:["/one","/two"],indexes:["/"],match:{type:"re2",pattern:"/.*\\.tar"}});
 expect(w.text()).toContain("任务读取上限");await w.setProps({application:"vendor/other"});await flushPromises();expect((w.findAll("textarea")[0]!.element as HTMLTextAreaElement).value).toBe("");w.unmount();
});
it("discards late options from a previous app",async()=>{
 resetStores();let finish:(v:ReturnType<typeof response>)=>void=()=>{};
 vi.stubGlobal("fetch",vi.fn((url:string)=>{
  if(url.endsWith("/configuration"))return Promise.resolve(response(configurationFixture({},1,"vendor/http")));
  if(url.includes("vendor/old/prewarm/options"))return new Promise(resolve=>{finish=resolve});return Promise.resolve(response({release:false,platforms:[],channels:[],limits}));
 }));
 const w=mount(PrewarmSettings,{props:{application:"vendor/old"}});await flushPromises();await w.setProps({application:"vendor/new"});await flushPromises();finish(response({release:true,platforms:[{id:"wrong",name:"wrong"}],channels:["latest"],limits}));await flushPromises();
 expect(w.findAll("textarea")).toHaveLength(2);expect(w.text()).not.toContain("wrong");w.unmount();
});
