(async()=>{
 if (location.pathname!=='/') return;
 const log=(o)=>fetch('/__probe/log',{method:'POST',body:JSON.stringify(o)});
 const r = await fetch('/__shell/download',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'/api/v1/export?x=1',filename:'via-route.csv'})});
 await log({route:r.status});
 const b = await fetch('/__shell/download',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({bytes_base64:btoa('hi'),filename:'bytes.csv',content_type:'text/csv'})});
 await log({bytes:b.status});
 setTimeout(()=>{ location.href='/api/v1/export?nav=1' },500);
 setTimeout(()=>log({navDone:location.pathname}),3000);
})()
