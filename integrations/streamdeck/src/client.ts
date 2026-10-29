import {randomUUID} from 'node:crypto';
import type {Snapshot} from './keys.js';

export type ConnectionSettings={endpoint?:string;username?:string;password?:string;allowInsecureHttp?:boolean};
export type Clip={id:string;name:string;state:string;duration:number};
export type Outcome={id:string;state:string;reason?:string};
export function shortError(reason:string){
  if(/OBS|keyframe|timed.out/i.test(reason))return 'NO OBS';
  if(/restart|changed|context/i.test(reason))return 'CHANGED';
  if(/auth/i.test(reason))return 'AUTH';
  if(/unknown|unavailable|offline/i.test(reason))return 'OFFLINE';
  if(/ending/i.test(reason))return 'ENDING';
  if(/start.*first/i.test(reason))return 'START FIRST';
  return 'NOT READY';
}
export function connectionSettings(value:ConnectionSettings):ConnectionSettings{
  const endpoint=new URL(value.endpoint||'');
  if(!['http:','https:'].includes(endpoint.protocol)||endpoint.username||endpoint.password||endpoint.search||endpoint.hash||endpoint.pathname!=='/')throw new Error('Use the server origin without a path or embedded credentials.');
  if(endpoint.protocol==='http:'&&!value.allowInsecureHttp)throw new Error('Use HTTPS, or explicitly allow HTTP on a trusted network.');
  if(typeof value.username!=='string'||!value.username||/[:\r\n]/.test(value.username)||typeof value.password!=='string'||!value.password)throw new Error('Enter the dashboard username and password.');
  return {endpoint:endpoint.origin,username:value.username,password:value.password,allowInsecureHttp:value.allowInsecureHttp===true};
}
export class Restreamer {
  settings:ConnectionSettings={};
  snapshot?:Snapshot;
  received=0;
  online=false;
  problem='SETUP';
  busy=false;
  cooldown=0;
  clips:Clip[]=[];
  private generation=0;
  private abort=new AbortController();
  private pending?:{id:string;server:string;report:(text:string)=>void};
  constructor(private changed:()=>void){}
  configure(settings:ConnectionSettings){
    this.generation++;this.abort.abort();this.abort=new AbortController();
    this.online=false;this.snapshot=undefined;this.pending=undefined;this.clips=[];
    try{this.settings=connectionSettings(settings);this.problem='CONNECT';}catch{this.settings={};this.problem='SETUP';}
    this.changed();
  }
  current(){return this.online&&Date.now()-this.received<3500?this.snapshot:undefined;}
  private async request(path:string,body?:object):Promise<{ok:boolean;status:number;data:any}>{
    const response=await fetch(new URL(path,this.settings.endpoint),{
      method:body?'POST':'GET',redirect:'error',cache:'no-store',signal:AbortSignal.any([this.abort.signal,AbortSignal.timeout(2500)]),
      headers:{Authorization:`Basic ${Buffer.from(`${this.settings.username}:${this.settings.password}`).toString('base64')}`,'Content-Type':'application/json','X-Restreamer-Control':'1'},
      body:body?JSON.stringify(body):undefined
    });
    if(response.status===401||response.status===403)throw new Error('AUTH');
    return {ok:response.ok,status:response.status,data:await response.json()};
  }
  async refresh(){
    const generation=this.generation;
    if(!this.settings.endpoint)return;
    try{
      const {ok,data}=await this.request('/api/dashboard');
      if(generation!==this.generation)return;
      if(!ok||!['OFF','PRESTREAM','LIVE','BRB','CLIP','ENDING'].includes(data.stage?.stage)||typeof data.stage.server_id!=='string'||typeof data.stage.context!=='string'||!Array.isArray(data.outputs)||data.outputs.some((o:any)=>!o||typeof o.enabled!=='boolean'||typeof o.can_enable!=='boolean'||typeof o.state!=='string'))throw new Error('STATUS');
      this.snapshot=data;this.received=Date.now();this.online=true;this.problem='';
      if(this.pending){
        if(this.pending.server!==data.stage.server_id){this.pending.report('RESTARTED');this.pending=undefined;}
        else{
          const result=data.stage.outcomes?.find((o:Outcome)=>o.id===this.pending?.id);
          if(result&&result.state!=='pending'){this.pending.report(result.state==='completed'?'':shortError(result.reason||result.state));this.pending=undefined;}
        }
      }
    }catch(error){if(generation===this.generation){this.online=false;this.problem=error instanceof Error&&error.message==='AUTH'?'AUTH':'OFFLINE';}}
    this.changed();
  }
  async library():Promise<Clip[]>{
    const generation=this.generation;
    const {ok,data}=await this.request('/api/library');
    if(generation!==this.generation)throw new Error('CHANGED');
    if(!ok||!data.enabled||!Array.isArray(data.revisions))throw new Error('NOT READY');
    this.clips=data.revisions.filter((c:any)=>typeof c.id==='string'&&typeof c.name==='string'&&typeof c.state==='string');
    return this.clips;
  }
  async execute(action:string,revision:string|undefined,name:string|undefined,report:(text:string)=>void){
    const stage=this.current()?.stage;
    if(!stage){report(this.problem||'OFFLINE');return;}
    if(action==='go_live'&&stage.pending){report('WAIT OBS');return;}
    if(this.busy||Date.now()<this.cooldown){report('BUSY');return;}
    this.busy=true;this.changed();
    const generation=this.generation;
    try{
      if(action==='play_clip'){
        const clips=await this.library();
        const clip=clips.find(c=>c.id===revision);
        if(!clip){report(revision?'MISSING':'SELECT CLIP');return;}
        if(clip.state!=='ready'){report('NOT READY');return;}
        if(name&&clip.name!==name){report('CHANGED');return;}
      }
      if(generation!==this.generation||!this.current()){report('CHANGED');return;}
      const id=randomUUID();
      // Keep identity only to reconcile outcomes. Never retry the POST.
      this.pending={id,server:stage.server_id,report};
      const {data,ok}=await this.request('/api/stage/commands',{id,server_id:stage.server_id,context:stage.context,action,confirmed:true,...(revision?{revision}:{})});
      if(generation!==this.generation)return;
      if(!ok||!['completed','pending'].includes(data.state)){report(shortError(data.reason||'Not ready'));this.pending=undefined;}
      else if(data.state==='completed'){report('');this.pending=undefined;}
      else report('WAIT OBS');
    }catch(error){
      if(generation===this.generation){this.online=false;this.problem=error instanceof Error&&error.message==='AUTH'?'AUTH':'OFFLINE';report(this.problem);}
    }finally{
      await this.refresh();this.cooldown=Date.now()+600;this.busy=false;this.changed();
    }
  }
  async poll(){await this.refresh();setTimeout(()=>void this.poll(),1000);}
}
