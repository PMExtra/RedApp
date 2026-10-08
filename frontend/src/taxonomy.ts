import { api } from "./api";
import type { Configuration } from "./configuration";
import type { LocalizedText } from "./site";
export type TaxonomyKind="categories"|"tags";
export type TaxonomyItem=Omit<Configuration,"proxy_effective"> & {kind:TaxonomyKind;id:string;name:LocalizedText;builtin:boolean};
export async function allTaxonomy(signal?:AbortSignal):Promise<TaxonomyItem[]>{
 const items:TaxonomyItem[]=[];
 for(let page=1;;page++){
  const result=await api<{items:TaxonomyItem[];total_pages:number}>(`taxonomy?page=${page}&limit=100`,undefined,signal);
  if(!Array.isArray(result.items))throw new Error("Taxonomy unavailable");items.push(...result.items);
  if(page>=result.total_pages) return items;
 }
}
