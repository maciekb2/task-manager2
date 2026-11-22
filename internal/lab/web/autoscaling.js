(() => {
 const samples=[];
 $('hpaBaseline').onclick=async()=>{try{if(state?.experiment?.active)throw Error('Najpierw zakończ eksperyment A/B.');await request('/api/action',{action:'reset'});await request('/api/action',{action:'scale',target:'echo',replicas:1});await request('/api/config',{rps:50,concurrency:20,delay_ms:0,cpu_ms:50,error_percent:0,payload_bytes:0,timeout_ms:0,max_in_flight:0,breaker:false,fan_out:false,duration_seconds:180});await refresh();notify('Jedna replika pod obciążeniem CPU. Zmierz punkt wyjścia przed włączeniem HPA.');}catch(e){notify(e.message,true);}};
 $('hpaEnable').onclick=()=>act({action:'autoscale-on'},'Włączono HPA echo. Czekaj na metryki i gotowe repliki');
 $('hpaDisable').onclick=()=>act({action:'autoscale-off'},'Usunięto HPA echo; przywracane są 2 repliki');
 window.labAutoscalingState=s=>{
  const h=(s.cluster?.autoscalers||[]).find(h=>h.name==='echo'),d=(s.cluster?.deployments||[]).find(d=>d.name==='echo'),until=s.config.autoscale_until||0;
  const recommendation=h&&h.desired_replicas>=h.min_replicas?h.desired_replicas:null;
  $('hpaStatus').textContent=h?`HPA: ${h.current_replicas??'—'} aktualnych → ${recommendation??'— (oczekiwanie na pomiar)'} rekomendowanych · limit ${h.min_replicas}–${h.max_replicas} · ostatnia zmiana ${time(h.last_scale)}${until?' · koniec demo za '+Math.max(0,Math.ceil(until-Date.now()/1000))+' s':''}`:until?'Zapisany zamiar HPA — czekam na potwierdzenie zasobu w discovery.':'HPA wyłączone. Skalowanie ręczne.';
  markup('hpaEvidence',h?table(['Warunek','Stan','Powód','Wyjaśnienie'],(h.conditions||[]).map(c=>[esc(c.type),esc(c.status),esc(c.reason),esc(c.message)]))+`<p class="muted">CPU raportowane przez HPA: <code>${esc(JSON.stringify(h.current_metrics||[]))}</code>. Cel: 200m CPU/pod (wartość bezwzględna, nie procent requests).</p>`:'');
  const at=Date.parse(s.cluster?.at||'');if(Number.isFinite(at)&&d&&samples.at(-1)?.at!==at){samples.push({at,ready:d.ready||0,desired:d.replicas||0,hpa:recommendation});if(samples.length>100)samples.shift();}
  if(!samples.length)return;
  const W=750,H=160,L=28,T=12,B=25,start=samples[0].at,end=Math.max(start+1000,samples.at(-1).at),x=t=>L+(t-start)/(end-start)*(W-L-12),y=v=>H-B-v/5*(H-T-B);
  const lines=[['desired','Zadane w Deployment','#6dacf5'],['ready','Rzeczywiście gotowe','#6fe5bd'],['hpa','Rekomendacja HPA','#f0bc6a']];
  markup('hpaChart',`<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Historia obserwacji skalowania echo">${[0,1,2,3,4,5].map(v=>`<text x="3" y="${y(v)+3}" class="axis-label">${v}</text><line x1="${L}" x2="${W-12}" y1="${y(v)}" y2="${y(v)}" stroke="#25303d"/>`).join('')}${lines.map(([key,label,color])=>{let pen=false,path='';for(const p of samples){if(p[key]===null){pen=false;continue;}path+=`${pen?'L':'M'}${x(p.at).toFixed(1)},${y(p[key]).toFixed(1)} `;pen=true;}return `<path d="${path}" fill="none" stroke="${color}" stroke-width="2"><title>${label}</title></path>`;}).join('')}<text x="${L}" y="${H-4}" class="axis-label">${time(start)}</text><text x="${W-12}" y="${H-4}" text-anchor="end" class="axis-label">${time(end)}</text></svg><div class="chart-legend">${lines.map(([,label,color])=>`<span><i style="background:${color}"></i>${label}</span>`).join('')}</div>`);
 };
})();
