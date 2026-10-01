export interface Resource {
  ID: string; State: string; Bytes: number; Readers: number; Retired: boolean;
  AverageBPS: number; RecentBPS: number; Resumes: number; VerificationNS: number;
  Started: string; Finished: string; Error: string;
  Resource: { Labels: {version: string; name: string} }
}
export interface Status {
  name: string; public_base_url: string; sampled_at: string; os: string; arch: string; go: string;
  goroutines: number; memory_bytes: number; client_runtime_update_policy: string;
  disk: {cache_bytes:number;temporary_bytes:number;pending_bytes:number;used_bytes:number;free_bytes:number};
  rates: {upstream_bytes_per_second:number;downstream_bytes_per_second:number};
  versions: Record<string,string>; counters: Record<string,number>; resources: Resource[];
  events: Array<{time:string;resource:string;category:string;message:string;status_code?:number}>;
}
export interface CleanupPreview {
  job: {ID:string;Selected:Array<{Resource:string;Generation:string}>};
  logical_bytes: number; active: number; unknown_versions: string[];
}
export interface ProxySettings { server: string; has_credentials: boolean; has_password: boolean; dns: string }
export class ApiError extends Error { constructor(message:string,public status:number){super(message)} }
let csrf = ''
export function setCSRF(value:string){csrf=value}
export async function api<T>(path:string,body?:unknown,signal?:AbortSignal):Promise<T>{
  const response=await fetch('/admin/api/'+path,{method:body===undefined?'GET':'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:body===undefined?undefined:JSON.stringify(body),signal})
  const data=await response.json()
  if(!response.ok)throw new ApiError(data.error || 'Request failed',response.status)
  return data as T
}
export function bytes(value:number|null|undefined):string {
  if(value===null||value===undefined||!Number.isFinite(value)||value<0)return '—'
  const units=['B','KiB','MiB','GiB','TiB']
  const index=value===0?0:Math.min(Math.floor(Math.log(value)/Math.log(1024)),units.length-1)
  return `${(value/1024**Math.max(0,index)).toFixed(2)} ${units[Math.max(0,index)]}`
}
