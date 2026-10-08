<script setup lang="ts">
import {computed,ref,nextTick} from "vue";
import {api} from "../api";
import {language,t,errorText} from "../i18n";
import {useNumberedCollection} from "../composables/useNumberedCollection";
import {useDirtyDraft} from "../composables/useDirtyDraft";
import type {TaxonomyItem,TaxonomyKind} from "../taxonomy";
import IconButton from "../components/IconButton.vue";
import PageNavigation from "../components/PageNavigation.vue";
import DeleteConfirmation from "../components/DeleteConfirmation.vue";
import OverrideControl from "../components/OverrideControl.vue";
const kind=ref<TaxonomyKind>("categories"),search=ref("");
const collection=useNumberedCollection<TaxonomyItem>(computed(()=>`taxonomy?kind=${kind.value}&q=${encodeURIComponent(search.value)}`));
const {items,page,total,totalPages,loading,error:listingError,refresh,go}=collection;
const selected=ref<TaxonomyItem>(),editing=ref(false),id=ref(""),name=ref({en:"","zh-CN":""}),busy=ref(false),error=ref<unknown>(),deleting=ref<TaxonomyItem>();
const touched=ref(new Set<string>()),unsets=ref(new Set<string>());
const dirty=computed(()=>editing.value && (!selected.value ? !!id.value||!!name.value.en||!!name.value["zh-CN"] : touched.value.size>0||unsets.value.size>0));
const confirmDiscard=useDirtyDraft(dirty);
function select(item?:TaxonomyItem){if(!confirmDiscard())return; selected.value=item;id.value=item?.id||"";name.value={...(item?.name||{en:"","zh-CN":""})};touched.value=new Set();unsets.value=new Set();editing.value=true;error.value=undefined;deleting.value=undefined;}
function changeKind(value:TaxonomyKind){if(!confirmDiscard())return;editing.value=false;selected.value=undefined;error.value=undefined;deleting.value=undefined;kind.value=value;search.value="";}
function moveTab(){changeKind(kind.value==="tags"?"categories":"tags");void nextTick(()=>document.getElementById(`taxonomy-${kind.value}-tab`)?.focus())}
function mark(path:string){touched.value.add(path);unsets.value.delete(path);}
function restore(path:string){const locale=path.slice(5) as "en"|"zh-CN";const defaults=selected.value?.defaults?.name as typeof name.value|undefined;if(defaults){name.value[locale]=defaults[locale];unsets.value.add(path);touched.value.delete(path);}}
async function reloadSelected(){if(!selected.value||!confirmDiscard())return;const response=await api<{items:TaxonomyItem[]}>(`taxonomy?kind=${kind.value}&q=${encodeURIComponent(selected.value.id)}&limit=100`);const current=response.items.find(x=>x.id===selected.value?.id);if(current){selected.value=current;name.value={...current.name};touched.value=new Set();unsets.value=new Set();error.value=undefined;}}
async function save(){if(busy.value || selected.value&&!dirty.value)return;busy.value=true;error.value=undefined;try{const set:Record<string,string>={};for(const path of touched.value)set[path]=name.value[path.slice(5) as "en"|"zh-CN"];
const result=await api<TaxonomyItem>(`taxonomy/${kind.value}${selected.value ? `/${selected.value.id}` : ""}`,selected.value?{revision:selected.value.revision,set,unset:[...unsets.value]}:{id:id.value,name:name.value},undefined,{},selected.value?"PATCH":"POST");selected.value=result;name.value={...result.name};touched.value=new Set();unsets.value=new Set();await refresh();}catch(reason){error.value=reason;}finally{busy.value=false;}}
async function remove(){if(!deleting.value)return;busy.value=true;error.value=undefined;try{await api(`taxonomy/${kind.value}/${deleting.value.id}`,{revision:deleting.value.revision},undefined,{},"DELETE");if(selected.value?.id===deleting.value.id)editing.value=false;deleting.value=undefined;await refresh();}catch(reason){error.value=reason;}finally{busy.value=false;}}
</script>
<template>
 <div class="page-heading"><h1>{{t("Manage categories and tags")}}</h1><IconButton icon="plus" :label="t('Create')" :disabled="busy" @click="select()" /></div>
 <div class="application-tabs taxonomy-tabs" role="tablist" :aria-label="t('Category and tags')"><button v-for="tab in (['categories','tags'] as const)" :key="tab" role="tab" :id="`taxonomy-${tab}-tab`" aria-controls="taxonomy-panel" :tabindex="kind===tab?0:-1" :aria-selected="kind===tab" @keydown.left.prevent="moveTab" @keydown.right.prevent="moveTab" :disabled="busy" @click="changeKind(tab)">{{t(tab==='categories'?'Categories':'Tags')}}</button></div>
 <label class="search-field"><span class="sr-only">{{t('Search by ID or name')}}</span><input v-model="search" type="search" maxlength="128" :placeholder="t('Search by ID or name')" /></label>
 <p v-if="error||listingError" class="error" role="alert">{{errorText(error||listingError)}}</p>
 <section id="taxonomy-panel" class="panel" role="tabpanel" :aria-labelledby="`taxonomy-${kind}-tab`"><div v-for="item in items" :key="item.id" class="directory-row"><button class="secondary" :disabled="busy" @click="select(item)">{{item.name[language]}} <small>{{item.id}}</small></button><IconButton v-if="!item.builtin" icon="trash" :label="t('Delete')" :disabled="busy" @click="deleting=item" /></div>
 <PageNavigation :label="t('Category and tags')" :page="page" :total="total" :total-pages="totalPages" :previous="page>1" :next="page<totalPages" :loading="loading" @go="go" @previous="go(page-1)" @next="go(page+1)" @refresh="refresh" /></section>
 <DeleteConfirmation v-if="deleting" :application="false" taxonomy :record-key="deleting.id" :busy="busy" @confirm="remove" @cancel="deleting=undefined" />
 <p v-if="selected?.template_missing" class="notice">{{t("Template unavailable; the last accepted defaults remain in use.")}}</p>
 <form v-if="editing" class="panel" @submit.prevent="save"><label>ID<input v-model="id" required pattern="[a-z0-9]+(-[a-z0-9]+)*" maxlength="63" :disabled="!!selected||busy" /></label>
 <label v-for="locale in (['en','zh-CN'] as const)" :key="locale">{{locale==='en'?'English':'简体中文'}}<input v-model="name[locale]" required maxlength="256" :disabled="busy" @input="mark(`name.${locale}`)" /><OverrideControl :configuration="selected" :path="`name.${locale}`" :custom="touched.has(`name.${locale}`)" :restored="unsets.has(`name.${locale}`)" :disabled="busy" @customize="mark(`name.${locale}`)" @restore="restore(`name.${locale}`)" /></label>
 <button :disabled="busy">{{t('Save')}}</button><IconButton v-if="selected" icon="refresh" :label="t('Reload')" :disabled="busy" @click="reloadSelected" /><button type="button" class="secondary" :disabled="busy" @click="confirmDiscard()&&(editing=false)">{{t('Cancel')}}</button></form>
</template>
