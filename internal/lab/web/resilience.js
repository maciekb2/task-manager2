(() => {
 let dirty=false,loading=false;
 $('resilienceSettings').addEventListener('input',()=>dirty=true);
 const fields=()=>({timeout_ms:+$('resTimeout').value,max_in_flight:+$('resLimit').value,breaker:$('resBreaker').value==='true',fan_out:$('resFanout').value==='true',branch_delay_ms:+$('resBranch').value});
 async function apply(extra){try{await request('/api/config',{...state.config,...extra,duration_seconds:extra.duration_seconds||60});dirty=false;await refresh();notify('Zastosowano ograniczony scenariusz. Sprawdź rzeczywiste odpowiedzi i trace.');}catch(e){notify(e.message,true);}}
 $('resilienceSettings').onsubmit=e=>{e.preventDefault();apply(fields());};
 const presets={timeout:{rps:10,concurrency:12,delay_ms:800,timeout_ms:150},pressure:{rps:20,concurrency:20,delay_ms:800,max_in_flight:2},breaker:{rps:10,concurrency:4,delay_ms:20,error_percent:100,breaker:true},fanout:{rps:5,concurrency:8,delay_ms:40,fan_out:true,branch_delay_ms:400}};
 document.querySelectorAll('[data-resilience]').forEach(b=>b.onclick=()=>apply({rps:5,concurrency:4,delay_ms:0,cpu_ms:0,error_percent:0,payload_bytes:0,timeout_ms:0,max_in_flight:0,breaker:false,fan_out:false,branch_delay_ms:0,...presets[b.dataset.resilience],duration_seconds:30}));
 $('resRecover').onclick=()=>apply({error_percent:0});$('resReset').onclick=()=>act({action:'reset'},'Przywrócono bazę');
 $('resTrace').onclick=()=>switchTab('performance');$('resMap').onclick=()=>switchTab('map');
 window.labResilienceState=s=>{
  const c=s.config;
  $('resConfig').textContent=`W klastrze: timeout ${c.timeout_ms||0} ms · limit ${c.max_in_flight||0}/pod · breaker ${c.breaker?'wł.':'wył.'} · fan-out ${c.fan_out?'wł.':'wył.'} · gałąź ${c.branch_delay_ms||0} ms${dirty?' · Formularz zawiera niezastosowany szkic.':''}`;
  if(!dirty&&!$('resilienceSettings').contains(document.activeElement)){for(const [key,input] of Object.entries({timeout_ms:'resTimeout',max_in_flight:'resLimit',breaker:'resBreaker',fan_out:'resFanout',branch_delay_ms:'resBranch'}))$(input).value=typeof c[key]==='boolean'?String(c[key]):c[key]||0;}
 };
 async function load(){if(loading||document.hidden||!$('view-resilience').classList.contains('active'))return;loading=true;try{const p=await request('/api/protection');markup('protectionStatus',table(['Pod','Stan','Praca w toku','Przyjęte','429 / limit','503 / breaker','Timeouty'],[[esc(p.pod),esc(p.state),p.active,p.admitted,p.backpressure_rejections,p.circuit_rejections,p.timeouts]]));}catch(e){$('protectionStatus').textContent=e.message;}finally{loading=false;}}
 window.addEventListener('lab-tab',load);setInterval(load,3000);
})();
