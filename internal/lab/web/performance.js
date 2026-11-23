(() => {
  let latest=null,currentState=null,pending=false,activeTrace='';
  function plot(id,points,lines,unit) {
    const values=points.flatMap(p=>lines.map(l=>l.get(p))).filter(v=>v!==null&&v!==undefined&&Number.isFinite(v));
    if(!values.length){$(id).innerHTML='<div class="empty">Brak świeżych próbek. Metrics Server potrzebuje chwili po uruchomieniu podów.</div>';return;}
    const W=550,H=190,left=48,right=12,top=12,bottom=34,max=Math.max(...values)*1.12||1;
    const series=lines.map(line=>{const byTime=new Map();for(const p of points){const raw=line.get(p),valid=raw!==null&&raw!==undefined&&Number.isFinite(raw);const measured=line.at?.(p);const at=Date.parse(valid&&measured&&!measured.startsWith('0001')?measured:p.at);if(Number.isFinite(at))byTime.set(at,{at,value:valid?raw:null});}return [...byTime.values()].sort((a,b)=>a.at-b.at);});
    const times=series.flatMap(rows=>rows.map(p=>p.at)),start=Math.min(...times),end=Math.max(...times);const x=t=>left+(t-start)/Math.max(end-start,1000)*(W-left-right),y=v=>H-bottom-v/max*(H-top-bottom);
    let svg=`<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Wykres ${esc(unit)}"><g>`;
    for(let i=0;i<4;i++){const value=max*i/3,yy=y(value);svg+=`<line x1="${left}" y1="${yy}" x2="${W-right}" y2="${yy}" stroke="#2b3845"/><text x="${left-7}" y="${yy+3}" text-anchor="end" class="axis-label">${value<10?value.toFixed(2):value.toFixed(0)}</text>`;}
    for(const [index,line] of lines.entries()){let d='',pen=false;for(const p of series[index]){const v=p.value;if(v===null||v===undefined||!Number.isFinite(v)){pen=false;continue;}d+=`${pen?'L':'M'}${x(p.at).toFixed(1)},${y(v).toFixed(1)} `;pen=true;}svg+=`<path d="${d}" fill="none" stroke="${line.color}" stroke-width="2"/>`;}
    svg+=`<text x="${left}" y="${H-7}" class="axis-label">${time(start)}</text><text x="${W-right}" y="${H-7}" text-anchor="end" class="axis-label">${time(end)}</text></g></svg>`;
    const template=document.createElement('template');template.innerHTML=svg+`<div class="chart-legend">${lines.map(l=>`<span><i style="background:${l.color}"></i>${esc(l.label)}</span>`).join('')}<span>${esc(unit)} · brakujące próbki pozostają przerwą · CPU/RAM: czas Metrics API</span></div>`;patchSVG($(id),template.content);
  }
  function render() {
    if(!latest)return;const component=$('perfService').value,points=latest.series||[],all=component==='all';
    const get=key=>p=>all?p[key]:p.components?.[component]?.[key];
    plot('cpuChart',points,[{label:all?'Suma podów laboratorium':component,color:'#6fe5bd',get:get('cpu_cores'),at:p=>all?p.metric_at:p.components?.[component]?.metric_at},...(all?[{label:'Węzeł Kubernetes (z control plane)',color:'#6dacf5',get:p=>p.node_cpu_cores,at:p=>p.node_metric_at}]:[])],'rdzenie CPU');
    plot('memoryChart',points,[{label:all?'Suma podów laboratorium':component,color:'#6fe5bd',get:get('memory_mib'),at:p=>all?p.metric_at:p.components?.[component]?.metric_at},...(all?[{label:'Węzeł Kubernetes',color:'#6dacf5',get:p=>p.node_memory_mib,at:p=>p.node_metric_at}]:[])],'MiB');
    plot('trafficChart',latest.traffic_series||points,[{label:'Cały łańcuch HTTP',color:'#6fe5bd',get:p=>p.rps}],'żądania/s');
    plot('latencyChart',latest.traffic_series||points,[{label:'P95 całego żądania',color:'#f0bc6a',get:p=>p.p95_ms}],'ms');
    const last=latest.latest,age=last?.metric_at&&!last.metric_at.startsWith('0001')?Math.max(0,Math.round((Date.now()-Date.parse(last.metric_at))/1000)):null;
    $('perfFreshness').textContent=age===null?'Brak próbek Metrics API':`Próbka sprzed ${age} s · pokrycie ${last.covered_pods}/${last.expected_pods} podów · historia do 15 min`;
    const rows=(latest.functions||[]).filter(f=>all||f.name.startsWith(component+'/'));
    markup('functionTable',table(['Usługa / funkcja','Wywołania','Średnio','P95','Maks.','Błędy'],rows.map(f=>[esc(f.name),f.count,`${f.mean_ms.toFixed(2)} ms`,`${f.p95_ms.toFixed(2)} ms`,`${f.max_ms.toFixed(2)} ms`,f.errors])));
    markup('podPerformance',table(['Pod','Komponent','CPU','RAM','Czas próbki','Okno pomiaru'],(last?.pods||[]).filter(p=>all||p.component===component).map(p=>[esc(p.name),esc(p.component),`${p.cpu_millicores.toFixed(2)} mCPU`,`${p.memory_mib.toFixed(1)} MiB`,time(p.timestamp),esc(p.window)])));
  }
  async function load(){if(pending)return;pending=true;try{latest=await request('/api/performance?minutes='+$('perfWindow').value);render();}catch(e){$('perfFreshness').textContent=e.message;}finally{pending=false;}}
  window.labPerformanceState=s=>{currentState=s;const select=$('perfService'),selected=select.value,names=(s.cluster?.deployments||[]).map(d=>d.component||d.name).sort();if(select.dataset.names!==names.join(',')){select.innerHTML='<option value="all">Całe laboratorium</option>'+names.map(n=>`<option value="${esc(n)}">${esc(n)}</option>`).join('');if(names.includes(selected))select.value=selected;select.dataset.names=names.join(',');}
    const roots=(s.events||[]).filter(e=>e.kind==='http'&&e.to==='gateway').slice(0,8);markup('requestTraces',roots.length?roots.map(e=>`<button class="trace-choice" data-otel-trace="${esc(e.trace)}"><span>${esc(e.from)} → HTTP ${e.status}</span><code>${esc(e.trace)}</code><strong>${e.duration_ms.toFixed(1)} ms</strong></button>`).join(''):'<p class="muted">Uruchom ruch lub zadanie. Nowe żądania pojawią się tutaj.</p>');
    $('requestTraces').querySelectorAll('[data-otel-trace]').forEach(b=>b.onclick=()=>showTrace(b.dataset.otelTrace));
    markup('timedTasks',table(['Zadanie','Status','Próby','Do pierwszego podjęcia','Do wyniku','Trace'],(s.queue?.tasks||[]).map(t=>[esc(t.id),badge(t.state),t.attempt,t.first_started&&!t.first_started.startsWith('0001')?`${t.queue_ms.toFixed(1)} ms`:'—',t.finished&&!t.finished.startsWith('0001')?`${t.total_ms.toFixed(1)} ms`:['completed','dead-letter'].includes(t.state)?'Brak pomiaru w starszym rekordzie':'W toku',`<button class="text-button" data-task-otel="${esc(t.trace)}">Otwórz →</button>`])));
    $('timedTasks').querySelectorAll('[data-task-otel]').forEach(b=>b.onclick=()=>showTrace(b.dataset.taskOtel));
    markup('timedJobs',table(['Job','Wynik','Start','Zakończenie','Czas','Trace próby'],(s.cluster?.jobs||[]).map(j=>{const finished=j.finished||(j.conditions||[]).find(c=>c.type==='Failed'&&c.status==='True')?.lastTransitionTime;const wall=j.started?((Date.parse(finished||s.at)-Date.parse(j.started))/1000).toFixed(1)+' s'+(finished?'':' (w toku)'):'—';const event=s.events.find(e=>e.kind==='job'&&e.pod.startsWith(j.name+'-'));return [esc(j.name),badge(j.succeeded?'completed':finished?'failed':'processing'),time(j.started),time(finished),wall,event?`<button class="text-button" data-job-otel="${esc(event.trace)}">Otwórz →</button>`:'Brak próby w ostatnich zdarzeniach'];})));
    $('timedJobs').querySelectorAll('[data-job-otel]').forEach(b=>b.onclick=()=>showTrace(b.dataset.jobOtel));
  };
  async function showTrace(id) {
    activeTrace=id;switchTab('performance');window.labPanels?.show('performance','trace');$('waterfall').innerHTML='<p class="muted">Pobieram rzeczywiste spany OpenTelemetry z Jaegera…</p>';
    try{
      const result=await request('/api/otel/trace?id='+encodeURIComponent(id));if(activeTrace!==id)return;
      const trace=result.data?.[0],spans=trace?.spans||[];const run=spans.flatMap(s=>s.tags||[]).find(t=>t.key==='lab.run.id'&&t.value)?.value;if(!spans.length)throw Error('Trace jeszcze nie dotarł albo wygasł. SDK eksportuje spany partiami; spróbuj ponownie za chwilę.');
      const byId=new Map(spans.map(s=>[s.spanID,s])),start=Math.min(...spans.map(s=>s.startTime)),end=Math.max(...spans.map(s=>s.startTime+s.duration)),total=end-start;
      const depth=(span,visited=new Set())=>{if(visited.has(span.spanID))return 0;visited.add(span.spanID);const ref=span.references?.find(r=>r.refType==='CHILD_OF'),parent=ref&&byId.get(ref.spanID);return parent?1+depth(parent,visited):0;};
      spans.sort((a,b)=>a.startTime-b.startTime||depth(a)-depth(b));
      $('waterfall').innerHTML=`<div class="panel-heading"><div><h3>Trace ${esc(id)}</h3><span class="muted">${run?`run_id ${esc(run)} · `:''}${spans.length} spanów · ${((end-start)/1000).toFixed(1)} ms od początku do końca · czasy zagnieżdżone</span></div><a href="/tracing/trace/${esc(id)}" target="_blank" rel="noopener">Jaeger ↗</a></div>`+spans.map(s=>{const service=trace.processes?.[s.processID]?.serviceName||'?',d=Math.min(10,depth(s)),error=s.tags?.some(t=>t.key==='otel.status_code'&&t.value==='ERROR');return `<div class="waterfall-row"><div style="padding-left:${d*12}px"><strong>${esc(service)}</strong><span>${esc(s.operationName)}</span></div><div class="waterfall-track"><span class="waterfall-bar ${error?'error':''}" style="left:${100*(s.startTime-start)/Math.max(total,1)}%;width:${Math.max(.3,100*s.duration/Math.max(total,1))}%"></span></div><code>${(s.duration/1000).toFixed(2)} ms</code></div>`;}).join('');
    }catch(e){if(activeTrace===id){$('waterfall').innerHTML=`<p class="muted">${esc(e.message)}</p><button class="secondary" id="retryTrace">Pobierz ponownie</button>`;$('retryTrace').onclick=()=>showTrace(id);}}
  }
  window.showPerformanceTrace=showTrace;
  $('perfWindow').onchange=load;$('perfService').onchange=render;
  window.addEventListener('lab-tab',e=>{if(e.detail==='performance')load();});
  setInterval(()=>{if(!document.hidden&&$('view-performance').classList.contains('active'))load();},5000);
})();
