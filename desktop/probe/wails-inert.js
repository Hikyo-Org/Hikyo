(async()=>{
 const out={path:location.pathname};
 out.wails = typeof window._wails;
 out.wailsKeys = window._wails? Object.keys(window._wails):null;
 const mh = window.webkit && window.webkit.messageHandlers;
 out.handlers = mh? Object.keys(mh):null;
 out.external = !!(mh && mh.external);
 // 403 alone is ambiguous: a window the manager does not recognise gets 403
 // too. The control passes the binding check; the /wails/ answers must carry
 // the runtime block's own message.
 const blocked = async (path) => { const r = await fetch(path); const b = await r.json().catch(()=>({})); return r.status===403 && /runtime is not exposed/.test(b.message||'') };
 try { out.control = (await fetch('/')).status } catch(e){out.control='err '+e}
 try { out.runtimeBlocked = await blocked('/wails/runtime.js') } catch(e){out.runtimeBlocked='err '+e}
 try { out.flagsBlocked = await blocked('/wails/flags') } catch(e){out.flagsBlocked='err '+e}
 try { out.invoke = (window._wails && window._wails.invoke) ? (window._wails.invoke('wails:window:close'), 'called') : 'absent' } catch(e){out.invoke='threw '+e}
 await fetch('/__probe/log',{method:'POST',body:JSON.stringify(out)});
 if (location.pathname==='/') setTimeout(()=>{location.href='/second'},500);
})()
