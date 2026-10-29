import streamDeck, {action, SingletonAction, type WillAppearEvent, type WillDisappearEvent, type KeyDownEvent, type KeyUpEvent, type KeyAction, type DidReceiveSettingsEvent} from '@elgato/streamdeck';
import {health, keySVG, labels, type Face} from './keys.js';
import {Restreamer, connectionSettings, type ConnectionSettings} from './client.js';

type ClipSettings={revision?:string;name?:string};
type Button={key:KeyAction<ClipSettings>;kind:string;settings:ClipSettings;image?:string;down?:number;context?:string;message?:string;until?:number};
const buttons=new Map<string,Button>();
const client=new Restreamer(render);
// Trace logging includes settings payloads. Never enable it, and never log
// server responses, URLs, headers or errors containing user-controlled data.
streamDeck.logger.setLevel('error');
function feedback(button:Button,text:string){button.message=text;button.until=Date.now()+4000;render();}
function render(){
  const snapshot=client.current();
  for(const b of buttons.values()){
    let face:Face=health(snapshot);
    if(b.kind!=='status'){
      const stage=snapshot?.stage;
      const active=stage?.stage===({'prestream':'PRESTREAM','go-live':'LIVE','brb-return':'BRB','end-stream':'ENDING','play-clip':'CLIP','stop-now':'OFF'} as Record<string,string>)[b.kind]&&(b.kind!=='play-clip'||stage?.media?.revision===b.settings.revision);
      face={label:labels[b.kind],detail:snapshot?'READY':client.problem||'OFFLINE',tone:'gray',symbol:snapshot?'○':'?',description:snapshot?'READY':'UNKNOWN'};
      if(active)face={...face,tone:'green',symbol:'✓',detail:'ACTIVE',description:'ACTIVE'};
      if(b.kind==='brb-return'&&stage?.stage==='BRB')face.label='RETURN';
      if(b.kind==='play-clip'&&!active&&snapshot)face.detail=b.settings.name?.slice(0,12)||'SELECT CLIP';
      if(b.kind==='go-live'&&stage?.pending)face={...face,tone:'blue',symbol:'…',detail:'WAIT OBS',description:'PENDING'};
      if(b.kind==='stop-now'){
        face.detail='HOLD 2s';face.tone='gray';face.symbol='■';
        if(b.down){face.detail=Date.now()-b.down>=2000?'RELEASE':'HOLD…';face.tone='amber';face.description='STOP CONFIRMATION';}
      }
      if(b.message&&Date.now()<(b.until||0))face={...face,detail:b.message,tone:'amber',symbol:'!',description:b.message};
      if(!snapshot)face={...face,tone:'gray',symbol:'?',detail:client.problem||'OFFLINE',description:'UNKNOWN'};
    }else if(!snapshot)face.detail=client.problem||'OFFLINE';
    const svg=keySVG(face);
    if(b.image!==svg){b.image=svg;void b.key.setImage(svg);}
  }
}
class Control extends SingletonAction<ClipSettings>{
  constructor(readonly kind:string){super();}
  override onWillAppear(ev:WillAppearEvent<ClipSettings>){
    if(ev.action.isKey()){buttons.set(ev.action.id,{key:ev.action,kind:this.kind,settings:ev.payload.settings});render();}
  }
  override onWillDisappear(ev:WillDisappearEvent<ClipSettings>){buttons.delete(ev.action.id);}
  override onDidReceiveSettings(ev:DidReceiveSettingsEvent<ClipSettings>){const b=buttons.get(ev.action.id);if(b){b.settings=ev.payload.settings;b.message='';b.down=undefined;render();}}
  override async onKeyDown(ev:KeyDownEvent<ClipSettings>){
    const b=buttons.get(ev.action.id);if(!b||b.down)return;
    b.down=Date.now();b.context=client.current()?.stage.context;
    if(this.kind==='status'){if(client.settings.endpoint)await streamDeck.system.openUrl(new URL('/live',client.settings.endpoint).href);return;}
    if(this.kind==='stop-now'){render();return;}
    const action=({'prestream':'prestream','go-live':'go_live','brb-return':client.current()?.stage.stage==='BRB'?'return':'brb','end-stream':'end_stream','play-clip':'play_clip'} as Record<string,string>)[this.kind];
    await client.execute(action,this.kind==='play-clip'?b.settings.revision:undefined,b.settings.name,text=>feedback(b,text));
  }
  override async onKeyUp(ev:KeyUpEvent<ClipSettings>){
    const b=buttons.get(ev.action.id);if(!b)return;
    const held=b.down?Date.now()-b.down:0;b.down=undefined;
    if(this.kind==='stop-now'&&held>=2000){
      if(!b.context||b.context!==client.current()?.stage.context){feedback(b,'CHANGED');return;}
      await client.execute('stop_now',undefined,undefined,text=>feedback(b,text));
    }
    render();
  }
}
@action({UUID:'com.kreuzhofer.restreamer.prestream'})class Prestream extends Control{constructor(){super('prestream');}}
@action({UUID:'com.kreuzhofer.restreamer.go-live'})class GoLive extends Control{constructor(){super('go-live');}}
@action({UUID:'com.kreuzhofer.restreamer.brb-return'})class Break extends Control{constructor(){super('brb-return');}}
@action({UUID:'com.kreuzhofer.restreamer.end-stream'})class Ending extends Control{constructor(){super('end-stream');}}
@action({UUID:'com.kreuzhofer.restreamer.play-clip'})class Clip extends Control{constructor(){super('play-clip');}}
@action({UUID:'com.kreuzhofer.restreamer.status'})class Status extends Control{constructor(){super('status');}}
@action({UUID:'com.kreuzhofer.restreamer.stop-now'})class Stop extends Control{constructor(){super('stop-now');}}
for(const control of [new Prestream(),new GoLive(),new Break(),new Ending(),new Clip(),new Status(),new Stop()])streamDeck.actions.registerAction(control);
streamDeck.settings.onDidReceiveGlobalSettings<ConnectionSettings>(ev=>{client.configure(ev.settings);});
streamDeck.ui.onSendToPlugin(async ev=>{
  const payload=ev.payload;
  if(!payload||typeof payload!=='object'||Array.isArray(payload))return;
  try{
    if(payload.command==='save'){
      const input=payload.settings;
      if(!input||typeof input!=='object'||Array.isArray(input))throw new Error('Invalid connection settings.');
      const next=connectionSettings({endpoint:typeof input.endpoint==='string'?input.endpoint:'',username:typeof input.username==='string'?input.username:'',password:typeof input.password==='string'&&input.password?input.password:client.settings.password,allowInsecureHttp:input.allowInsecureHttp===true});
      await streamDeck.settings.setGlobalSettings(next);client.configure(next);await client.refresh();
    }
    if(payload.command==='load'||payload.command==='save'){
      // Keep credentials exclusively in global settings; the inspector only
      // learns whether a password is saved, never its value.
      const config={endpoint:client.settings.endpoint||'',username:client.settings.username||'',hasPassword:!!client.settings.password,allowInsecureHttp:!!client.settings.allowInsecureHttp};
      let libraryError='';
      let choices:{id:string;name:string;state:string;duration:number}[]=[];
      try{choices=await client.library();}catch{libraryError='Library unavailable. Prepare BRB and videos in Restreamer, then refresh.';}
      await streamDeck.ui.sendToPropertyInspector({config,clips:choices,message:client.current()?'Connected. Relay status is current.':client.problem==='AUTH'?'Authentication failed. Check dashboard credentials.':'Status unavailable. Check the server address and connection.',libraryError});
    }
  }catch(error){await streamDeck.ui.sendToPropertyInspector({error:error instanceof Error?error.message:'Could not save settings.'});}
});
await streamDeck.connect();
client.configure(await streamDeck.settings.getGlobalSettings<ConnectionSettings>());
setInterval(render,250);
void client.poll();
