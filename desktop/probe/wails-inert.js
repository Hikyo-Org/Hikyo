(async()=>{
 const out={path:location.pathname};
 out.wails = typeof window._wails;
 out.wailsKeys = window._wails? Object.keys(window._wails):null;
 const mh = window.webkit && window.webkit.messageHandlers;
 out.handlers = mh? Object.keys(mh):null;
 out.external = !!(mh && mh.external);
 try { out.runtime = (await fetch('/wails/runtime.js')).status } catch(e){out.runtime='err '+e}
 try { out.flags = (await fetch('/wails/flags')).status } catch(e){out.flags='err '+e}
 try { out.invoke = (window._wails && window._wails.invoke) ? (window._wails.invoke('wails:window:close'), 'called') : 'absent' } catch(e){out.invoke='threw '+e}
 await fetch('/__probe/log',{method:'POST',body:JSON.stringify(out)});
 if (location.pathname==='/') setTimeout(()=>{location.href='/second'},500);
})()
