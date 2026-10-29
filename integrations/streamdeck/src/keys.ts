export type Stage = {
  stage:string; server_id:string; context:string; source:string; mode:string;
  pending?:{id:string}; error?:string; fallback_error?:string; return_stage?:string;
  media?:{revision?:string}; outcomes?:{id:string;state:string;reason?:string}[];
};
export type Snapshot = {stage:Stage; outputs:{enabled:boolean;can_enable:boolean;state:string;last_error?:string}[]};
export type Face = {label:string; detail:string; tone:'gray'|'green'|'amber'|'red'|'blue'; symbol:string; description:string};
export const labels:Record<string,string> = {prestream:'PRE', 'go-live':'LIVE', 'brb-return':'BRB', 'end-stream':'END', 'play-clip':'CLIP', status:'UNKNOWN', 'stop-now':'STOP'};

export function health(snapshot:Snapshot | undefined):Face {
  if (!snapshot) return {label:'UNKNOWN',detail:'OFFLINE',tone:'gray',symbol:'?',description:'UNKNOWN'};
  const stage=snapshot.stage;
  const label=({PRESTREAM:'PRE',ENDING:'END'} as Record<string,string>)[stage.stage] || stage.stage;
  // Same eligible set and reconnect semantics as the Live overview: saved
  // failures do not make OFF/rehearsal red; reconnecting after failure does.
  const intended=snapshot.outputs.filter(o=>o.enabled&&o.can_enable);
  const sending=intended.filter(o=>o.state==='streaming').length;
  const failed=intended.some(o=>o.state==='retrying'||(o.last_error&&['connecting','waiting_for_keyframe'].includes(o.state)));
  const detail=`${sending}/${intended.length}`;
  if(stage.stage==='OFF')return {label,detail:'OFF',tone:'gray',symbol:'—',description:'OFF'};
  if(stage.mode==='preview_only')return {label,detail:'REHEARSE',tone:'blue',symbol:'○',description:'REHEARSAL'};
  if(!intended.length)return {label,detail:'NO DEST',tone:'amber',symbol:'!',description:'NO DESTINATIONS'};
  if(!sending&&!failed&&!stage.error&&!stage.fallback_error)return {label,detail,tone:'blue',symbol:'…',description:'STARTING'};
  if(!sending||stage.error||stage.fallback_error)return {label,detail,tone:'red',symbol:'×',description:'FAILED'};
  if(sending<intended.length||(stage.stage==='LIVE'&&stage.source!=='obs'))return {label,detail,tone:'amber',symbol:'!',description:'DEGRADED'};
  return {label,detail,tone:'green',symbol:'✓',description:'HEALTHY'};
}
const colors={gray:'#9aa8b8',green:'#5de59c',amber:'#ffd166',red:'#ff747b',blue:'#79bdff'};
const escape=(text:string)=>text.replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&apos;'}[c]!));
export function keySVG(face:Face):string {
  const color=colors[face.tone];
  const icons:Record<string,string>={
    '✓':'<path d="m28 15 6 6 12-14"/>',
    '!':'<path d="m36 5 13 20H23Z"/><path d="M36 12v5m0 4v.1"/>',
    '×':'<path d="m28 7 16 16m0-16L28 23"/>',
    '?':'<circle cx="36" cy="15" r="11"/><path d="M32 11c0-5 10-5 8 1-1 3-4 2-4 6m0 4v.1"/>',
    '…':'<circle cx="29" cy="15" r="1"/><circle cx="36" cy="15" r="1"/><circle cx="43" cy="15" r="1"/>',
    '○':'<circle cx="36" cy="15" r="9"/>',
    '—':'<path d="M27 15h18"/>',
    '■':'<rect x="28" y="7" width="16" height="16"/>'
  };
  return `<svg xmlns="http://www.w3.org/2000/svg" width="144" height="144" viewBox="0 0 72 72"><title>${escape(face.description)}</title><rect width="72" height="72" rx="9" fill="#101722"/><rect x="2" y="2" width="68" height="68" rx="7" fill="none" stroke="${color}" stroke-width="3"/><g fill="${color}" text-anchor="middle" font-family="Arial, sans-serif" font-weight="700"><g fill="none" stroke="${color}" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">${icons[face.symbol]||icons['?']}</g><text x="36" y="44" font-size="${face.label.length>5?12:19}">${escape(face.label)}</text><text x="36" y="61" font-size="${face.detail.length>7?9:12}">${escape(face.detail)}</text></g></svg>`;
}
