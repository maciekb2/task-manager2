(() => {
 const settings=(scenario,duration=60)=>({...scenarios[scenario],payload_bytes:0,duration_seconds:duration});
 async function traffic(scenario,view) {
  try{await request('/api/config',settings(scenario));await refresh();if(view)switchTab(view);notify('Demo uruchomione. Porównuj rzeczywiste dane i czasy pomiarów.');}catch(e){notify(e.message,true);}
 }
 $('portfolioNormal').onclick=()=>traffic('normal','performance');$('portfolioSlow').onclick=()=>traffic('slow','performance');$('portfolioCPU').onclick=()=>traffic('cpu','performance');
 $('portfolioQueue').onclick=()=>switchTab('queue');$('portfolioLearn').onclick=()=>switchTab('learn');
 $('portfolioTraffic').onclick=async()=>{try{await request('/api/config',settings('normal',90));await refresh();notify('Ruch na 90 s. Teraz możesz porównać wersję bazową i błędną.');}catch(e){notify(e.message,true);}};
 $('portfolioRelease').onclick=()=>act({action:'release-demo'},'Zlecono zmianę obrazu echo. Sprawdź gotowe pody i rzeczywiste HTTP');
 $('portfolioRollback').onclick=()=>act({action:'rollback-demo'},'Zlecono przywrócenie obrazu bazowego. Potwierdź świeże HTTP 200');
 $('portfolioReset').onclick=()=>act({action:'reset'},'Zakończono demo i przywrócono bazę');
 window.labPortfolioUpdate=s=>{
  const deployment=(s.cluster?.deployments||[]).find(d=>d.name==='echo'),pods=(s.cluster?.pods||[]).filter(p=>p.component==='echo');
  const images=deployment?.images||[],fault=images.some(i=>i.endsWith('-demo-fault')),remaining=Math.max(0,Math.ceil((s.config.release_until||0)-Date.now()/1000));
  const e=(s.events||[]).find(e=>e.kind==='http'&&e.to==='echo');
  $('releaseStatus').textContent=`Deployment echo: ${deployment?.ready||0}/${deployment?.replicas||0} ready · ${fault?'zadany obraz z błędem':'zadany obraz bazowy'}${remaining?' · automatyczny rollback za '+remaining+' s':''}${e?' · ostatni pomiar HTTP '+e.status+' o '+time(e.time):' · brak świeżej odpowiedzi do sprawdzenia'}`;
  $('releaseStatus').classList.toggle('fault',fault);
  markup('releaseEvidence',`<p class="muted">Obraz zadany w Deployment: <code>${esc(images.join(', ')||'Oczekiwanie na discovery')}</code></p>`+table(['Rzeczywisty pod','Gotowość','Obraz kontenera','Identyfikator obrazu'],pods.map(p=>[esc(p.name),`${p.ready}/${p.containers}`,`<code>${esc((p.images||[]).join(', '))}</code>`,`<code>${esc((p.image_ids||[]).join(', ')||'Oczekiwanie na start')}</code>`])));
 };
})();
