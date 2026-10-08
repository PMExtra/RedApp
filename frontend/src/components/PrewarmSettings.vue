<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from "vue";
import {api,bytes,isCancellation} from "../api";
import {appAPI} from "../bootstrap";
import {useConfiguration} from "../composables/useConfiguration";
import {errorText,t,messages,type Message} from "../i18n";
import OverrideControl from "./OverrideControl.vue";
import PageNavigation from "./PageNavigation.vue";
const props=defineProps<{application:string}>();
type Limits={max_files:number;max_depth:number;max_download_bytes:number;max_duration_seconds:number};
type Policy={enabled:boolean;channels:string[];platforms:string[]};
type Options={release:boolean;platforms:{id:string;name:string}[];channels:string[];limits:Limits};
type Job={id:string;state:string;completed:number;succeeded:number;bytes:number;reason?:string};
type Item={key:string;status:string;reason?:string;bytes:number};
const {draft,configuration,touched,unsets,loading,saving,error,saved,save,mark,restore}=useConfiguration<{prewarm?:Policy}>(computed(()=>`apps/${props.application}/configuration`),"",["prewarm"]);
const options=ref<Options>(),target=ref(""),platforms=ref<string[]>([]),paths=ref(""),indexes=ref(""),match=ref(""),matchKind=ref("glob"),limits=ref<Limits>({max_files:10000,max_depth:16,max_download_bytes:10*1024**3,max_duration_seconds:3600});
const job=ref<Job>(),items=ref<Item[]>([]),page=ref(1),total=ref(0),totalPages=ref(1),busy=ref(false),requestError=ref<unknown>();
let ticket=0,controller:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined;
const running=computed(()=>job.value?.state==="running");
function label(value:string){return value in messages ? t(value as Message) : value}
function id(){return Array.from(crypto.getRandomValues(new Uint8Array(16)),n=>n.toString(16).padStart(2,"0")).join("")}
function invalidate(){ticket++;controller?.abort();controller=undefined;clearTimeout(timer);busy.value=false}
async function request(action:"start"|"status"|"cancel"|"retry"|"page",requested=1){
 if(busy.value||!options.value)return;
 const app=props.application,attempt=++ticket,signal=(controller=new AbortController()).signal;
 busy.value=true;requestError.value=undefined;
 try{
  let value:Job|undefined;
  if(action==="start"){
   const input=options.value.release?{target:target.value,platforms:platforms.value}:{paths:paths.value.split(/\r?\n/).filter(Boolean),indexes:indexes.value.split(/\r?\n/).filter(Boolean),...(match.value?{match:{type:matchKind.value,pattern:match.value}}:{})};
   value=await api<Job>(`${appAPI(app)}/prewarm/start`,{request_id:id(),...input,limits:limits.value},signal);requested=1;
  }else if(job.value){
   const base=`${appAPI(app)}/prewarm/${job.value.id}`;
   if(action==="cancel")await api(`${base}/cancel`,{},signal);
   if(action==="retry"){value=await api<Job>(`${base}/retry`,{request_id:id()},signal);requested=1}
   else value=await api<Job>(base,undefined,signal);
  }
  if(attempt!==ticket||app!==props.application)return;
  if(value){
   job.value=value;try{localStorage.setItem(`prewarm:${app}`,value.id)}catch{/* Optional resume. */}
   const result=await api<{items:Item[];total:number;total_pages:number}>(`${appAPI(app)}/prewarm/${value.id}/items?page=${requested}`,undefined,signal);
   if(attempt!==ticket)return;
   items.value=result.items;total.value=result.total;totalPages.value=result.total_pages;page.value=requested;
  }
 }catch(reason){if(attempt===ticket&&!isCancellation(reason))requestError.value=reason}
 finally{if(attempt===ticket){busy.value=false;controller=undefined;if(running.value){clearTimeout(timer);timer=setTimeout(()=>void request("status",page.value),1500)}}}
}
async function load(){
 invalidate();const app=props.application,attempt=ticket;
 options.value=undefined;job.value=undefined;items.value=[];platforms.value=[];target.value="";paths.value="";indexes.value="";match.value="";requestError.value=undefined;page.value=1;total.value=0;totalPages.value=1;
 const signal=(controller=new AbortController()).signal;
 try{
  const value=await api<Options>(`${appAPI(app)}/prewarm/options`,undefined,signal);
  if(attempt!==ticket||app!==props.application)return;
  options.value=value;limits.value={...value.limits};target.value=value.channels?.[0]||"";
  let previous:string|null=null;try{previous=localStorage.getItem(`prewarm:${app}`)}catch{/* Optional resume. */}
  if(previous&&/^[0-9a-f]{32}$/.test(previous)){const previousJob=await api<Job>(`${appAPI(app)}/prewarm/${previous}`,undefined,signal);if(attempt!==ticket)return;job.value=previousJob;void request("status")}
 }catch(reason){if(attempt===ticket&&!isCancellation(reason))requestError.value=reason}
}
async function upload(event:Event){
 const file=(event.target as HTMLInputElement).files?.[0],attempt=ticket;if(!file)return;
 try{if(file.size>2*1024**2)throw new Error(t("Path manifest must be UTF-8 and at most 2 MiB."));const text=new TextDecoder("utf-8",{fatal:true}).decode(await file.arrayBuffer());if(attempt===ticket)paths.value=text}
 catch(reason){if(attempt===ticket)requestError.value=reason}
}
watch(()=>props.application,()=>void load(),{immediate:true});onUnmounted(invalidate);
</script>
<template>
 <section class="panel">
  <h2>{{t("Prewarm cache")}}</h2>
  <p class="muted">{{t("Prewarming reads verified upstream files without increasing public access counts. One task runs at a time.")}}</p>
  <p v-if="error||requestError" class="error" role="alert">{{errorText(error||requestError)}}</p><p v-if="saved" role="status">{{t("Changes saved.")}}</p>
  <form v-if="options" @submit.prevent="request('start')">
   <template v-if="options.release">
    <label>{{t("Exact version or channel")}}<input v-model="target" required /></label>
    <fieldset><legend>{{t("Platforms")}}</legend><label v-for="platform in options.platforms" :key="platform.id"><input v-model="platforms" type="checkbox" :value="platform.id" />{{platform.name}}</label></fieldset>
   </template>
   <template v-else>
    <label>{{t("File paths, one per line")}}<textarea v-model="paths" placeholder="/file.tar.gz" /></label>
    <label>{{t("Directory indexes, one root per line")}}<textarea v-model="indexes" placeholder="/" /></label>
    <label>{{t("UTF-8 path manifest")}}<input type="file" accept="text/plain,.txt" @change="upload" /></label>
    <label>{{t("Optional discovered-file filter")}}<select v-model="matchKind"><option value="glob">glob</option><option value="re2">RE2</option></select><input v-model="match" /></label>
    <p class="muted">{{t("Use application-relative paths. Without indexes, a filter selects existing cached paths only.")}}</p>
   </template>
   <details><summary>{{t("Advanced task limits")}}</summary>
    <label>{{t("Maximum files")}}<input v-model.number="limits.max_files" type="number" min="1" max="100000" required /></label>
    <label>{{t("Maximum directory depth")}}<input v-model.number="limits.max_depth" type="number" min="0" max="32" required /></label>
    <label>{{t("Task read limit (bytes)")}}<input v-model.number="limits.max_download_bytes" type="number" min="1" :max="1024**4" required /></label>
    <p class="muted">{{t("This limits bytes read by this task. Shared upstream work may continue for public requests after this task stops.")}}</p>
    <label>{{t("Maximum duration (seconds)")}}<input v-model.number="limits.max_duration_seconds" type="number" min="1" max="86400" required /></label>
   </details>
   <button :disabled="busy||running||(options.release&&!platforms.length)">{{t("Start prewarming")}}</button>
  </form>
  <form v-if="options?.release&&draft?.prewarm" @submit.prevent="save">
   <h3>{{t("Automatic prewarming")}}</h3><p class="muted">{{t("Selected channels are checked on the next 15-minute cycle. Unchanged successful resources are skipped.")}}</p>
   <label><input v-model="draft.prewarm.enabled" type="checkbox" @change="mark('prewarm')" />{{t("Enable automatic prewarming")}}</label>
   <fieldset><legend>{{t("Channels")}}</legend><label v-for="channel in options.channels" :key="channel"><input v-model="draft.prewarm.channels" type="checkbox" :value="channel" @change="mark('prewarm')" />{{channel}}</label></fieldset>
   <fieldset><legend>{{t("Platforms")}}</legend><label v-for="platform in options.platforms" :key="platform.id"><input v-model="draft.prewarm.platforms" type="checkbox" :value="platform.id" @change="mark('prewarm')" />{{platform.name}}</label></fieldset>
   <OverrideControl :configuration="configuration" path="prewarm" :custom="touched.has('prewarm')" :restored="unsets.has('prewarm')" :disabled="loading||saving" @restore="restore('prewarm')" @customize="mark('prewarm')" />
   <button :disabled="loading||saving||(draft.prewarm.enabled&&(!draft.prewarm.channels.length||!draft.prewarm.platforms.length))">{{t("Save")}}</button>
  </form>
  <div v-if="job" class="cleanup-review" aria-live="polite">
   <p>{{label(job.state)}} · {{job.succeeded}}/{{job.completed}} · {{bytes(job.bytes)}} <span v-if="job.reason">· {{label(job.reason)}}</span></p>
   <button v-if="running" class="secondary" :disabled="busy" @click="request('cancel',page)">{{t("Cancel task")}}</button><button v-else class="secondary" :disabled="busy" @click="request('retry')">{{t("Retry failed or unfinished items")}}</button>
   <div class="table-scroll"><table><thead><tr><th>{{t("Path")}}</th><th>{{t("Status")}}</th><th>{{t("Size")}}</th></tr></thead><tbody><tr v-for="item in items" :key="item.key"><td>{{item.key}}</td><td>{{label(item.status)}}<span v-if="item.reason"> · {{label(item.reason)}}</span></td><td>{{bytes(item.bytes)}}</td></tr></tbody></table></div>
   <PageNavigation :label="t('Files')" :page="page" :previous="page>1" :next="page<totalPages" :loading="busy" :total="total" :total-pages="totalPages" @previous="request('page',page-1)" @next="request('page',page+1)" @go="request('page',$event)" @refresh="request('status',page)" />
  </div>
 </section>
</template>
