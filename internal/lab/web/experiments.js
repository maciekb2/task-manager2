(() => {
 let data={experiments:[],active:''},selected='',pending=false;
 const number=(v,d=2)=>v===null||v===undefined||!Number.isFinite(Number(v))?'—':Number(v).toLocaleString('pl-PL',{maximumFractionDigits:d});
 const settings=()=>({config:{timeout_ms:+$('expTimeout').value,max_in_flight:+$('expLimit').value,breaker:$('expBreaker').value==='true',fan_out:$('expFanout').value==='true',branch_delay_ms:+$('expBranch').value,rps:+$('expRPS').value,concurrency:+$('expConcurrency').value,delay_ms:+$('expDelay').value,cpu_ms:+$('expCPU').value,error_percent:+$('expErrors').value,payload_bytes:+$('expPayload').value},duration_seconds:+$('expDuration').value,replicas:+$('expReplicas').value});
 function record(){return data.experiments.find(e=>e.id===selected);}
 function plot(p) {
  const points=p.series||[];if(!points.length)return '<p class="muted">Próba jeszcze nie ma pomiarów HTTP.</p>';
  const values=points.map(x=>x.mean_ms).filter(x=>x!==null),max=Math.max(1,...values),W=750,H=135,L=40,T=12,B=24;
  const start=Date.parse(p.started),end=Math.max(start+1000,...points.map(x=>Date.parse(x.at))),x=t=>L+(t-start)/(end-start)*(W-L-12),y=v=>H-B-v/max*(H-T-B);
  let path='',pen=false;for(const v of points){if(v.mean_ms===null){pen=false;continue;}path+=`${pen?'L':'M'}${x(Date.parse(v.at)).toFixed(1)},${y(v.mean_ms).toFixed(1)} `;pen=true;}
  const actions=(p.timeline||[]).filter(e=>e.kind==='action');
  return `<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Średni czas odpowiedzi i akcje próby ${esc(p.label)}"><line x1="${L}" y1="${H-B}" x2="${W-12}" y2="${H-B}" stroke="#344354"/><text x="4" y="${T+5}" class="axis-label">${number(max,0)}ms</text><path d="${path}" fill="none" stroke="#6fe5bd" stroke-width="2"/>${actions.map(e=>`<line x1="${x(Date.parse(e.time))}" x2="${x(Date.parse(e.time))}" y1="${T}" y2="${H-B}" stroke="#f0bc6a" stroke-dasharray="3 5"><title>${esc(e.message)}</title></line>`).join('')}<text x="${L}" y="${H-5}" class="axis-label">${time(start)}</text><text x="${W-12}" y="${H-5}" text-anchor="end" class="axis-label">${time(end)}</text></svg>`;
 }
 function render() {
  const select=$('experimentSelect'),old=selected;
  const options=data.experiments.map(e=>`<option value="${esc(e.id)}">${esc(e.name)} · ${esc(e.state)}</option>`).join('');
  if(select.dataset.options!==options){select.innerHTML=options;select.dataset.options=options;}
  if(!data.experiments.some(e=>e.id===selected))selected=data.active||data.experiments[0]?.id||'';select.value=selected;
  const r=record(),phases=r?.phases||[],p=phases.at(-1),active=r?.id===data.active,running=p&&['running','preparing','draining'].includes(p.state);
  $('experimentStatus').textContent=r?`${r.state}${p?' · '+p.label+': '+p.state:''}${p?.state==='running'?' · '+Math.max(0,Math.ceil((Date.parse(p.deadline)-Date.now())/1000))+' s':''}`:'Utwórz eksperyment';
  $('experimentBaseline').disabled=!active||phases.length!==0;
  $('experimentVariant').disabled=!active||phases.length!==1||p?.state!=='completed';
  $('experimentFinish').disabled=!active||p?.state!=='running';$('experimentAbort').disabled=!active;$('experimentClose').disabled=!active||running;
  $('experimentExport').hidden=!r;$('experimentExport').href='/api/experiments/export?id='+encodeURIComponent(r?.id||'');
  const metrics=[['Stan',p=>esc(p.state)+(p.partial?' · niepełny':'' )],['run_id',p=>`<code>${esc(p.id)}</code>`],['Źródło / obraz',p=>`<code>${esc(p.version)}<br>${esc(p.image)}</code>`],['Ochrona / fan-out',p=>`${p.config.timeout_ms||0} ms timeout / ${p.config.max_in_flight||0} miejsc / breaker ${p.config.breaker?'wł.':'wył.'} / fan-out ${p.config.fan_out?'wł.':'wył.'} (${p.config.branch_delay_ms||0} ms)`],['Żądania/s zadane',p=>p.config.rps],['Współbieżność',p=>p.config.concurrency],['Opóźnienie / CPU / błędy',p=>`${p.config.delay_ms} ms / ${p.config.cpu_ms} ms / ${p.config.error_percent}%`],['Dodatkowy payload / repliki',p=>`${p.config.payload_bytes} B / ${p.replicas}`],['Rozpoczęte / zakończone żądania',p=>`${p.summary?.started??0} / ${p.summary?.count??0}`],['Czas obserwacji',p=>number(p.summary?.duration_seconds)+' s'],['Zmierzona przepustowość',p=>number(p.summary?.rps)+' req/s'],['Średnio / P95',p=>`${number(p.summary?.mean_ms)} / ${number(p.summary?.p95_ms)} ms${p.summary?.p95_complete===false&&p.summary?.count?' (próba ograniczona)':''}`],['Błędy odpowiedzi',p=>p.summary?.errors??'—'],['Odsetek błędów',p=>number(p.summary?.error_percent)+'%'],['Transfer / wszystkie odcinki',p=>number(p.summary?.payload_bytes_per_second/1024)+' KiB/s'],['Pełne próbki zasobów',p=>p.resources?.length||0]];
  const headers=['Miara',...phases.map(p=>p.label==='baseline'?'Baseline':'Wariant')],rows=metrics.map(([label,get])=>[esc(label),...phases.map(get)]);
  if(phases.length===2){headers.push('Różnica wariant − baseline');const a=phases[0].summary||{},b=phases[1].summary||{},keys={'Zmierzona przepustowość':['rps',' req/s'],'Odsetek błędów':['error_percent',' p.p.'],'Transfer / wszystkie odcinki':['payload_bytes_per_second',' KiB/s']};for(let i=0;i<metrics.length;i++){const spec=keys[metrics[i][0]];let diff='—';if(spec&&a.complete&&b.complete&&a[spec[0]]!==null&&b[spec[0]]!==null){const n=(b[spec[0]]-a[spec[0]])/(spec[0]==='payload_bytes_per_second'?1024:1);diff=(n>0?'+':'')+number(n)+spec[1];}rows[i].push(diff);}}
  markup('experimentComparison',r&&phases.length?table(headers,rows):'<p class="empty">Utwórz eksperyment, zmierz baseline i porównaj jeden wariant.</p>');
  const warnings=r?.persistence_error?['Błąd ostatniego zapisu historii. Wyeksportuj aktualny widok przed restartem.']:[];warnings.push(...phases.filter(p=>p.partial||p.note).map(p=>`${p.label}: ${p.note||'niepełny pomiar'}`));
  if(phases.some(p=>(p.timeline||[]).some(e=>e.message?.startsWith('Konfiguracja:'))))warnings.push('W trakcie próby zmieniano konfigurację. Tabela pokazuje ustawienia startowe, a oś czasu kolejne akcje.');
  if(phases.length===2&&phases[0].version!==phases[1].version)warnings.push('Pomiędzy próbami zmieniła się wersja kodu. Uwzględnij to w interpretacji.');
  if(phases.length&&phases.some(p=>!p.resources?.length))warnings.push('Brak kwalifikujących się pełnych okien CPU/RAM dla części prób. Nie porównujemy ich jako zer.');
  $('experimentCaveat').textContent=warnings.join(' ');
  const timeline=phases.map(p=>`<div class="experiment-phase-chart"><h3>${esc(p.label)} · ${time(p.started)} — ${p.ended&&!p.ended.startsWith('0001')?time(p.ended):'trwa'}</h3>${plot(p)}</div>`).join('');
  markup('experimentTimeline',timeline+table(['Czas','Próba','Zaobserwowana akcja / zdarzenie'],phases.flatMap(p=>(p.timeline||[]).map(e=>[time(e.time),esc(p.label),esc(e.message||e.kind)]))));
  markup('experimentTraces',phases.map(p=>`<h3>${esc(p.label)} · <code>${esc(p.id)}</code></h3>`+(p.traces||[]).map(id=>`<button class="trace-choice" data-experiment-trace="${esc(id)}"><code>${esc(id)}</code><span>Otwórz rzeczywisty waterfall →</span></button>`).join('')).join(''));
  $('experimentTraces').querySelectorAll('[data-experiment-trace]').forEach(b=>b.onclick=()=>window.showPerformanceTrace?.(b.dataset.experimentTrace));
  markup('experimentResources',table(['Próba','Czas próbki','CPU podów','RAM podów','Pokrycie'],phases.flatMap(p=>(p.resources||[]).map(s=>[esc(p.label),time(s.metric_at),number(s.cpu_cores)+' cores',number(s.memory_mib)+' MiB',`${s.covered_pods}/${s.expected_pods}`]))));
 }
 async function load(){if(pending)return;pending=true;try{data=await request('/api/experiments');render();}catch(e){notify(e.message,true);}finally{pending=false;}}
 async function action(name) {
  if(['baseline','variant'].includes(name)&&!$('experimentSettings').reportValidity())return;
  try{data=await request('/api/experiments',{action:name,id:selected,name:$('experimentName').value,...settings()});if(name==='create')selected=data.active;render();await refresh();}catch(e){notify(e.message,true);}
 }
 $('experimentCreate').onsubmit=e=>{e.preventDefault();action('create');};$('experimentSettings').onsubmit=e=>e.preventDefault();
 for(const [id,name] of [['experimentBaseline','baseline'],['experimentVariant','variant'],['experimentFinish','finish'],['experimentAbort','abort'],['experimentClose','close']])$(id).onclick=()=>action(name);
 $('experimentSelect').onchange=()=>{selected=$('experimentSelect').value;render();};
 window.labExperimentState=s=>{
  const e=s.experiment||{},bar=$('experimentContext');bar.hidden=!e.active;
  if(e.active)bar.textContent=`Eksperyment: ${e.name}${e.phase?' · '+e.phase.label+' / '+e.phase.state+' · '+e.phase.id:''}. Panele klastra pokazują dane globalne; porównanie run_id jest w Eksperymentach A/B.`;
 };
 window.addEventListener('lab-tab',e=>{if(e.detail==='experiments')load();});
 setInterval(()=>{if(!document.hidden&&$('view-experiments').classList.contains('active'))load();},3000);
})();
