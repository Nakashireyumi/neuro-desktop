package main

import (
	"crypto/sha256"
	"encoding/base64"
)

// Credentials live in the URL fragment until JavaScript removes it, then only
// in memory. The feed token cannot call the dashboard or publish events. Text is
// inserted through textContent, never interpreted as HTML.
const viewerScript = `(()=>{
const args=new URLSearchParams(location.hash.slice(1));
const id=args.get('integration')||'vencord-discord',token=args.get('token')||'';
history.replaceState(null,'',location.pathname);
const out=document.querySelector('main'),status=document.querySelector('#status');
let timer,stopped=false;
function clear(){out.replaceChildren();status.textContent='';}
async function refresh(){
  try {
    if(!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)||!token) {clear();return;}
    const response=await fetch('/api/viewer/'+encodeURIComponent(id),{headers:{'X-ND-Viewer-Token':token},cache:'no-store',signal:AbortSignal.timeout(5000)});
    if(!response.ok)throw Error('Feed unavailable');
    const data=await response.json();
    if(!data.visible){clear();}else{
      const rows=(data.items||[]).slice(-5).map(item=>{
        const row=document.createElement('div');row.className='event';
        const actor=document.createElement('strong');actor.textContent=item.actor==='evil'?'Evil':'Neuro';
        const text=document.createElement('span');text.textContent=String(item.summary||'');
        row.append(actor,text);return row;
      });out.replaceChildren(...rows);status.textContent='';
    }
  }catch{clear();}finally{if(!stopped)timer=setTimeout(refresh,2000);}
}
addEventListener('pagehide',()=>{stopped=true;clearTimeout(timer);clear();});
refresh();
})();`

func viewerScriptHash() string {
	sum := sha256.Sum256([]byte(viewerScript))
	return base64.StdEncoding.EncodeToString(sum[:])
}
func viewerOverlayHTML() string {
	return `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>Neuro Desktop activity</title><style>body{margin:0;background:transparent;color:white;font:22px system-ui}main{display:grid;gap:10px;padding:16px}.event{padding:14px 18px;background:#171827eb;border-radius:12px;display:grid;gap:5px}.event strong{color:#c7b6ff;font-size:16px}#status{display:none}</style><main aria-live="polite"></main><p id="status"></p><script>` + viewerScript + `</script></html>`
}
