<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from "vue";
import {allTaxonomy,type TaxonomyItem} from "../taxonomy";
import {isCancellation} from "../api";
import {useConfiguration} from "../composables/useConfiguration";
import {errorText,language,t} from "../i18n";
import SelectMenu from "./SelectMenu.vue";
import OverrideControl from "./OverrideControl.vue";
const props=defineProps<{application:string;readonly?:boolean}>();
const {draft,configuration,touched,unsets,loading,saving,error,saved,save,mark,restore,load}=useConfiguration<{category:string;tags:string[]}>(computed(()=>`apps/${props.application}/configuration`),"",["category","tags"]);
const entries=ref<TaxonomyItem[]>([]),listError=ref<unknown>();let controller:AbortController|undefined,ticket=0;
watch(()=>props.application,async()=>{controller?.abort();const attempt=++ticket;controller=new AbortController();entries.value=[];listError.value=undefined;try{const value=await allTaxonomy(controller.signal);if(attempt===ticket)entries.value=value}catch(reason){if(attempt===ticket&&!isCancellation(reason))listError.value=reason}},{immediate:true});
onUnmounted(()=>{ticket++;controller?.abort()});
const categories=computed(()=>[{value:"",label:t("Uncategorized")},...entries.value.filter(x=>x.kind==="categories").map(x=>({value:x.id,label:x.name[language.value]}))]);
const tags=computed(()=>entries.value.filter(x=>x.kind==="tags"));
function chooseTag(id:string,event:Event){if(!draft.value)return;const selected=new Set(draft.value.tags);if((event.target as HTMLInputElement).checked)selected.add(id);else selected.delete(id);draft.value.tags=[...selected].sort();mark("tags")}
</script>
<template><section class="panel"><h2>{{t("Category and tags")}}</h2>
 <p v-if="error||listError" class="error" role="alert">{{errorText(error||listError)}}</p><p v-if="saved" role="status">{{t("Changes saved.")}}</p>
 <p v-if="configuration?.template_missing" class="notice">{{t("Template unavailable; the last accepted defaults remain in use.")}}</p>
 <form v-if="draft" @submit.prevent="save">
  <label>{{t("Category")}}<SelectMenu :model-value="draft.category" :options="categories" :label="t('Category')" :disabled="readonly||loading||saving||!!listError" @update:model-value="draft.category=$event;mark('category')" /></label>
  <OverrideControl :configuration="configuration" path="category" :custom="touched.has('category')" :restored="unsets.has('category')" :disabled="readonly||loading||saving" @restore="restore('category')" @customize="mark('category')" />
  <fieldset><legend>{{t("Tags")}}</legend><label v-for="tag in tags" :key="tag.id"><input type="checkbox" :checked="draft.tags.includes(tag.id)" :disabled="readonly||loading||saving" @change="chooseTag(tag.id,$event)" />{{tag.name[language]}}</label><p v-if="!tags.length" class="muted">{{t("No tags are defined.")}}</p></fieldset>
  <OverrideControl :configuration="configuration" path="tags" :custom="touched.has('tags')" :restored="unsets.has('tags')" :disabled="readonly||loading||saving" @restore="restore('tags')" @customize="mark('tags')" />
  <button :disabled="readonly||loading||saving||!!listError">{{t("Save")}}</button><button type="button" class="secondary" :disabled="loading||saving" @click="load()">{{t("Reload")}}</button>
 </form></section></template>
