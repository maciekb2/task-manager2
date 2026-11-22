'use strict';
const $ = id => document.getElementById(id);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
let state = null, token = '', selected = '', trace = '', busy = false, toastTimer;
const editable = new Set(['generator','gateway','validator','enricher','echo','consumer','result-store']);
const positions = {web:[105,65],control:[325,65],observer:[545,65],jaeger:[705,65],'result-store':[875,65],generator:[105,265],gateway:[300,265],validator:[495,265],enricher:[690,265],echo:[885,265],job:[105,465],nats:[495,465],consumer:[885,465],inventory:[690,365],pricing:[885,365],'dead-letter':[885,595]};
const titles = {inventory:'Inventory',pricing:'Pricing',jaeger:'Jaeger',web:'Web UI',control:'Control API',observer:'Observer',generator:'Traffic generator',gateway:'Gateway',validator:'Validator',enricher:'Enricher',echo:'HTTP worker',nats:'NATS JetStream',consumer:'Queue consumer','result-store':'Result store',job:'Jobs / CronJobs','dead-letter':'Dead-letter'};
const colors = {web:'#88a5f2',control:'#88a5f2',observer:'#88a5f2',generator:'#6fe5bd',gateway:'#6fe5bd',validator:'#6fe5bd',enricher:'#6fe5bd',echo:'#6fe5bd',nats:'#f0bc6a',consumer:'#f0bc6a','result-store':'#b99df6',job:'#f0bc6a'};
function notify(message, error=false) { $('toast').textContent=message; $('toast').className='show'+(error?' error':''); clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').className='',6000); }
async function request(path, body, retry=true) {
  const options = body === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json','X-Lab-Token':token},body:JSON.stringify(body)};
  const response=await fetch(path,options);const data=await response.json();
  if(response.status===403&&body!==undefined&&retry){token=(await request('/api/session')).token;return request(path,body,false);}
  if(!response.ok) throw new Error(data.error||`HTTP ${response.status}`);return data;
}
async function act(body, message='Wykonano akcję') {try{const result=await request('/api/action',body);notify(result.name?`${message}: ${result.name}`:message);await refresh();return result;}catch(e){notify(e.message,true);}}
function switchTab(tab) {
  if(!document.getElementById(`view-${tab}`))tab='portfolio';
  const changed=!$(`view-${tab}`).classList.contains('active');
  $('viewTitle').textContent=document.querySelector(`.nav[data-tab="${tab}"] > span:last-child`).textContent;
  if(location.hash!==`#${tab}`)history.pushState(null,'',`#${tab}`);
  document.querySelectorAll('.nav').forEach(e=>{if(e.dataset.tab===tab)e.setAttribute('aria-current','page');else e.removeAttribute('aria-current');});
  document.body.classList.toggle('learning-mode',tab==='learn'||tab==='portfolio'||tab==='experiments'||tab==='resilience');document.querySelectorAll('[data-tab]').forEach(e=>e.classList.toggle('active',e.dataset.tab===tab));document.querySelectorAll('.view').forEach(e=>e.classList.toggle('active',e.id===`view-${tab}`));if(changed&&state)render();window.dispatchEvent(new CustomEvent('lab-tab',{detail:tab}));}
window.addEventListener('popstate',()=>switchTab(location.hash.slice(1)||'portfolio'));
document.querySelectorAll('[data-tab]').forEach(e=>e.addEventListener('click',()=>switchTab(e.dataset.tab)));
for(const name of ['rps','concurrency']) $(name).addEventListener('input',()=>$(name+'Value').value=$(name).value);
const trafficInputs={payload_bytes:'payload',rps:'rps',concurrency:'concurrency',delay_ms:'delay',error_percent:'errors',cpu_ms:'cpu',duration_seconds:'duration'};
const trafficDirty=new Set();
for(const input of Object.values(trafficInputs))$(input).addEventListener('input',()=>{trafficDirty.add(input);syncTrafficForm();});
function syncTrafficForm(){
 if(!state)return;
 for(const [key,input] of Object.entries(trafficInputs)){
  if(trafficDirty.has(input)||document.activeElement===$(input))continue;
  const value=state.config[key];
  if(value!==undefined&&(key!=='duration_seconds'||value>0))$(input).value=value;
 }
 for(const name of ['rps','concurrency'])$(name+'Value').value=$(name).value;
 $('trafficDraft').textContent=trafficDirty.size?'Szkic zmian — jeszcze niezastosowany. Stop zatrzymuje aktualną konfigurację.':'Konfiguracja z klastra. Czas dotyczy następnego uruchomienia; odliczanie pokazuje stan bieżący.';
}
function values() {return {...state?.config,payload_bytes:+$('payload').value,rps:+$('rps').value,concurrency:+$('concurrency').value,delay_ms:+$('delay').value,error_percent:+$('errors').value,cpu_ms:+$('cpu').value,duration_seconds:+$('duration').value};}
async function configure(config) {try{await request('/api/config',config);trafficDirty.clear();notify(config.rps?'Eksperyment uruchomiony. Obserwuj rzeczywisty ruch.':'Generator zatrzymany. Żądania już przyjęte kończą pracę.');await refresh();}catch(e){notify(e.message,true);}}
$('trafficForm').addEventListener('submit',e=>{e.preventDefault();configure(values());});
$('stopTraffic').addEventListener('click',async()=>{try{await request('/api/config',{...state.config,rps:0});await refresh();notify('Zatrzymano ruch. Szkic pozostaje niezastosowany.');}catch(e){notify(e.message,true);}});
const scenarios={normal:{rps:5,concurrency:4,delay_ms:40,error_percent:0,cpu_ms:0},slow:{rps:10,concurrency:12,delay_ms:800,error_percent:0,cpu_ms:0},errors:{rps:5,concurrency:4,delay_ms:40,error_percent:50,cpu_ms:0},cpu:{rps:20,concurrency:12,delay_ms:0,error_percent:0,cpu_ms:40}};
document.querySelectorAll('[data-scenario]').forEach(button=>button.addEventListener('click',()=>{const c=scenarios[button.dataset.scenario];for(const [key,input] of Object.entries({rps:'rps',concurrency:'concurrency',delay_ms:'delay',error_percent:'errors',cpu_ms:'cpu'}))$(input).value=c[key];$('rpsValue').value=c.rps;$('concurrencyValue').value=c.concurrency;configure({...c,payload_bytes:+$('payload').value,duration_seconds:+$('duration').value});}));
$('reset').addEventListener('click',()=>act({action:'reset'},'Przywrócono konfigurację, repliki i wstrzymano CronJob. Historia pozostała.'));
$('pauseConsumer').addEventListener('click',()=>act({action:'scale',target:'consumer',replicas:0},'Konsument wygaszany; przyjęte zadania mogą jeszcze kończyć pracę.'));
$('resumeConsumer').addEventListener('click',()=>act({action:'scale',target:'consumer',replicas:1},'Konsument wraca do pracy'));
$('queueForm').addEventListener('submit',e=>{e.preventDefault();act({action:'enqueue',count:+$('taskCount').value,failures:+$('taskFailures').value},'Zadania opublikowane');});
$('successJob').addEventListener('click',()=>act({action:'job',failures:0},'Utworzono Job'));
$('failedJob').addEventListener('click',()=>act({action:'job',failures:1},'Utworzono Job z kontrolowanym błędem'));
$('startCron').addEventListener('click',()=>act({action:'cron',suspended:false,schedule:$('schedule').value},'Harmonogram włączony'));
$('stopCron').addEventListener('click',()=>act({action:'cron',suspended:true,schedule:$('schedule').value},'Harmonogram wstrzymany'));
$('graphMetric').addEventListener('change',()=>state&&renderGraph());
$('declared').addEventListener('change',()=>state&&renderGraph());$('graphScope').addEventListener('change',()=>state&&renderGraph());
$('loadResults').addEventListener('click',async()=>{try{const rows=await request('/api/results');$('resultTable').innerHTML=table(['Czas','Ślad','HTTP','Czas całego łańcucha'],rows.map(e=>[time(e.time),`<button class="text-button" data-result-trace="${esc(e.trace)}">${esc(e.trace)}</button>`,`<span class="code ${e.status>=400?'bad':''}">${e.status}</span>`,`${e.duration_ms.toFixed(1)} ms`]));$('resultTable').querySelectorAll('[data-result-trace]').forEach(b=>b.addEventListener('click',()=>renderTrace(b.dataset.resultTrace)));}catch(e){notify(e.message,true);}});
const renderedMarkup=new WeakMap();
function markup(id,html) {
  const el=$(id);if(renderedMarkup.get(el)===html||el.contains(document.activeElement))return false;
  const top=el.scrollTop,left=el.scrollLeft;el.innerHTML=html;renderedMarkup.set(el,html);el.scrollTop=top;el.scrollLeft=left;return true;
}
// Reconcile keyed SVG elements in place: polling must not restart animation or lose keyboard focus.
function patchSVG(parent,source) {
  const desired=[...source.childNodes],existing=[...parent.childNodes];
  const key=n=>n.nodeType===1?n.getAttribute('data-key'):null;
  let cursor=parent.firstChild;
  for(let i=0;i<desired.length;i++) {
    const next=desired[i],id=key(next);
    let node=id?existing.find(n=>key(n)===id):existing[i];
    if(!node||node.nodeType!==next.nodeType||node.nodeName!==next.nodeName||(!id&&key(node)))node=next.cloneNode(true);
    if(node!==cursor)parent.insertBefore(node,cursor);
    if(node.nodeType===1){for(const attr of [...node.attributes])if(!next.hasAttribute(attr.name))node.removeAttribute(attr.name);for(const attr of next.attributes)if(node.getAttribute(attr.name)!==attr.value)node.setAttribute(attr.name,attr.value);patchSVG(node,next);}
    else if(node.nodeValue!==next.nodeValue)node.nodeValue=next.nodeValue;
    cursor=node.nextSibling;
  }
  while(cursor){const next=cursor.nextSibling;cursor.remove();cursor=next;}
}
function table(headers, rows) {return rows.length?`<table><thead><tr>${headers.map(h=>`<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${rows.map(row=>`<tr>${row.map(cell=>`<td>${cell}</td>`).join('')}</tr>`).join('')}</tbody></table>`:'<div class="empty">Brak danych. Uruchom scenariusz.</div>';}
function badge(value) {return `<span class="badge ${esc(value)}">${esc(value)}</span>`;}
function time(value) {return value?new Date(value).toLocaleTimeString('pl-PL'): '—';}
function deployments() {return state?.cluster?.deployments||[];}
function podList(component) {return (state?.cluster?.pods||[]).filter(p=>p.component===component);}
function usageFor(pod) {const usage=(state?.cluster?.usage||[]).find(p=>p.metadata?.name===pod);if(!usage)return '';return (usage.containers||[]).map(c=>`CPU ${c.usage?.cpu||'—'} · RAM ${c.usage?.memory||'—'}`).join(', ');}
function renderGraph() {
  $('graphObservedLegend').textContent=$('graphMetric').value==='transfer'?'Payload / 30 s · strzałka HTTP: odpowiedź wraca do klienta':'Liczba przejść / 30 s · animacja: ruch w ostatnich 5 s';
  const nodes=deployments().map(d=>({...d,id:d.component||d.name}));
  if((state.cluster?.cronjobs||[]).length) nodes.push({id:'job',name:'job',ready:(state.cluster?.jobs||[]).filter(j=>j.succeeded>0).length,replicas:(state.cluster?.jobs||[]).length});
  if(state.queue?.dlq_observed)nodes.push({id:'dead-letter',name:'DLQ'});
  const simple=$('graphScope').value==='flow';if(simple){for(let i=nodes.length-1;i>=0;i--)if(!['generator','gateway','validator','enricher','echo'].includes(nodes[i].id))nodes.splice(i,1);}const present=new Set(nodes.map(n=>n.id));
  nodes.forEach((n,i)=>{if(!positions[n.id])positions[n.id]=[105+(i%5)*195,565+Math.floor(i/5)*100];});
  const edgeMap=new Map();
  if($('declared').checked) for(const node of nodes) for(const to of node.dependencies||[]) if(to&&present.has(to))edgeMap.set(`${node.id}→${to}`,{from:node.id,to,count:0});
  for(const e of Object.values(state.edges||{})) {const to=e.to;if(present.has(e.from)&&present.has(to)&&e.from!==to)edgeMap.set(`${e.from}→${to}`,{...e,to});}
  let svg='<svg viewBox="'+(simple?'0 175 1000 180':'0 0 1000 655')+'" role="group" aria-label="Wykryte mikroserwisy i ich połączenia"><defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#657b8d"/></marker></defs>';
  svg+='<g data-key="edges">';
  for(const edge of edgeMap.values()) {
    const p=positions[edge.from],q=positions[edge.to];if(!p||!q)continue;
    const dx=q[0]-p[0],dy=q[1]-p[1],horizontal=Math.abs(dx)>Math.abs(dy);const start=[p[0]+(horizontal?Math.sign(dx)*77:0),p[1]+(horizontal?0:Math.sign(dy)*31)];const end=[q[0]-(horizontal?Math.sign(dx)*77:0),q[1]-(horizontal?0:Math.sign(dy)*31)];
    const curve=`M${start} C${horizontal?(start[0]+end[0])/2:start[0]},${horizontal?start[1]:(start[1]+end[1])/2} ${horizontal?(start[0]+end[0])/2:end[0]},${horizontal?end[1]:(start[1]+end[1])/2} ${end}`;
    const transfer=$('graphMetric').value==='transfer',measurement=(state.network?.edges||[]).find(e=>e.from===edge.from&&e.to===edge.to);
    const response=transfer&&measurement?.protocol==='http-response';
    const age=Date.now()-Date.parse(edge.last_event_at),fresh=age>=0&&age<5000;
    svg+=`<g data-key="${esc(edge.from+'→'+edge.to)}"><path data-last-event="${esc(edge.last_event_at||'')}" class="edge ${edge.count?'observed':''} ${fresh?'hot':''} ${edge.errors?'bad':''}" d="${curve}" ${response?'marker-start':'marker-end'}="url(#arrow)" style="${response?'animation-direction:reverse':''}"/><title>${esc(edge.from)} → ${esc(edge.to)}: ${edge.count||0} przejść / 30 s${edge.last_event_at?' · ostatnie '+time(edge.last_event_at):' · zależność z konfiguracji'}</title>`;
    if(edge.count)svg+=`<text class="edge-label" text-anchor="middle" x="${(start[0]+end[0])/2}" y="${(start[1]+end[1])/2-(transfer&&horizontal?40:7)}">${transfer?(measurement?`${(measurement.bytes_per_second/1024).toFixed(2)} KiB/s`:'bez pomiaru'):`${esc(edge.count)} ${edge.kind==='http'?'req':'events'}`}</text>`;
    svg+='</g>';
  }
  svg+='</g><g data-key="nodes">';
  for(const n of nodes) {const p=positions[n.id],healthy=n.id==='job'||n.id==='dead-letter'||(n.ready>=n.replicas&&n.replicas>0),color=colors[n.id]||'#6dacf5';svg+=`<g data-key="${esc(n.id)}" class="node ${selected===n.id?'selected':''}" data-node="${esc(n.id)}" role="button" tabindex="0" aria-label="${esc(titles[n.id]||n.name)}" transform="translate(${p[0]-77},${p[1]-31})"><rect width="154" height="62" rx="9"/><circle cx="14" cy="18" r="3.5" fill="${healthy?color:'#f7868b'}"/><text class="node-name" x="25" y="23">${esc(titles[n.id]||n.name)}</text><text class="node-type" x="14" y="45">${n.id==='dead-letter'?`${state.queue.dead_letters} wiadomości · stream DLQ`:n.id==='job'?`${n.ready} zakończone / ${n.replicas} zadań`:`${n.ready||0} / ${n.replicas||0} ready · :${n.id==='nats'?'4222':'8080'}`}</text></g>`;}
  const template=document.createElement('template');template.innerHTML=svg+'</g></svg>';patchSVG($('graph'),template.content);

}
function chooseNode(event) {
  const node=event.target.closest('[data-node]');if(!node)return;
  if(event.type==='keydown'&&!['Enter',' '].includes(event.key))return;
  event.preventDefault();selected=node.dataset.node;renderInspector();renderGraph();
}
setInterval(()=>{if(document.hidden||!$('view-map').classList.contains('active'))return;for(const edge of $('graph').querySelectorAll('[data-last-event]')){const age=Date.now()-Date.parse(edge.dataset.lastEvent);edge.classList.toggle('hot',age>=0&&age<5000);}},1000);
$('graph').addEventListener('click',chooseNode);$('graph').addEventListener('keydown',chooseNode);
function renderInspector() {
  if(!selected)return;const node=deployments().find(d=>(d.component||d.name)===selected);const pods=podList(selected);const panel=$('inspector');panel.classList.add('has-selection');
  let content=`<div class="section-label">SERVICE INSPECTOR</div><h3>${esc(titles[selected]||selected)}</h3><p class="muted">${esc(node?.description||(selected==='dead-letter'?'Stream JetStream tasks.dlq. Trafiają tu zadania po wyczerpaniu prób; to zasób brokera, bez osobnych podów.':'Jednorazowe i cykliczne wykonania kontrolowane przez Kubernetes.'))}</p>`;
  if(node)content+=`<p class="muted">Deployment: ${esc(node.name)}<br>Gotowe repliki: ${node.ready||0} / ${node.replicas||0}<br>Zależności: ${esc((node.dependencies||[]).filter(Boolean).join(', ')||'Kubernetes API')}</p>`;
  if(selected==='generator')content+='<p class="muted">Jedna replika generatora utrzymuje wspólny limit ruchu eksperymentu. Skaluj usługi obsługujące żądania.</p>';
  if(editable.has(selected)&&selected!=='generator')content+=`<form id="scaleForm" class="field-pair"><div><label for="replicaCount">Liczba replik</label><input id="replicaCount" type="number" min="0" max="5" value="${node?.replicas||0}" required></div><button class="secondary">Zastosuj</button></form>`;
  content+=pods.map(p=>`<div class="pod"><div class="pod-name">${esc(p.name)}</div><div class="pod-meta">${esc(p.phase)} · ready ${p.ready}/${p.containers} · restarty ${p.restarts}<br>${esc(p.ip||'bez IP')}<br>${esc(usageFor(p.name))}<br>Obraz: ${esc((p.images||[]).join(', '))}</div><div class="pod-actions"><button class="secondary" data-log="${esc(p.name)}">Logi</button>${editable.has(selected)?`<button class="secondary" data-restart="${esc(p.name)}">Odtwórz pod</button>`:''}</div></div>`).join('');
  const draft=panel.dataset.component===selected&&panel.dataset.replicas===String(node?.replicas||0)?$('replicaCount')?.value:undefined;
  if(!markup('inspector',content))return;
  panel.dataset.component=selected;panel.dataset.replicas=String(node?.replicas||0);
  if(draft!==undefined&&$('replicaCount'))$('replicaCount').value=draft;
  $('scaleForm')?.addEventListener('submit',e=>{e.preventDefault();act({action:'scale',target:selected,replicas:+$('replicaCount').value},'Zlecono zmianę liczby replik');});
  panel.querySelectorAll('[data-restart]').forEach(b=>b.addEventListener('click',()=>act({action:'restart',target:b.dataset.restart},'Pod usunięty; Deployment utworzy następcę')));
  panel.querySelectorAll('[data-log]').forEach(b=>b.addEventListener('click',async()=>{try{const v=await request('/api/logs?pod='+encodeURIComponent(b.dataset.log));$('logTitle').textContent=b.dataset.log;$('logText').textContent=v.logs;$('logDialog').showModal();}catch(e){notify(e.message,true);}}));
}
$('closeLogs').addEventListener('click',()=>$('logDialog').close());
async function renderTrace(id) {trace=id;try{const events=await request('/api/trace?id='+encodeURIComponent(id));$('traceView').innerHTML=`<button class="secondary" id="fullTrace">Otwórz pełny trace OpenTelemetry →</button>`+`<div class="trace-id">${esc(id)}</div>`+events.map(e=>`<div class="trace-row"><span>${esc(e.from)} → ${esc(e.to)}</span><span class="code ${e.status>=400?'bad':''}">${e.status||e.kind} · ${e.duration_ms.toFixed(1)} ms</span><small>${esc(e.message||e.operation||e.target_pod||e.pod)}${e.attempt?' · próba '+e.attempt:''}</small></div>`).join('');$('fullTrace').onclick=()=>window.showPerformanceTrace?.(id);}catch(e){notify(e.message,true);}}
function render() {
  const c=state.cluster,config=state.config,m=state.metrics,queue=state.queue,pods=c?.pods||[];
  syncTrafficForm();
  const stale=!c||Date.now()-Date.parse(c.at)>20000;
  $('connectionDot').className='status-dot'+(!stale?' connected':'');$('connection').textContent=stale?'Odkrywanie klastra / dane nieaktualne':'Połączono · systems-lab';$('clusterVersion').textContent=c?.version||'Minikube';
  markup('metricRPS',`${m.rps.toFixed(1)}<small>req/s</small>`);$('metricRequests').textContent=`${m.requests} żądań w ostatnich 30 s`;markup('metricLatency',`${m.requests?m.p95_ms.toFixed(0):'—'}<small>ms</small>`);markup('metricErrors',`${m.requests?(100*m.errors/m.requests).toFixed(0):0}<small>%</small>`);$('metricPods').textContent=`${pods.filter(p=>p.phase==='Running'&&p.ready===p.containers).length} / ${pods.filter(p=>p.phase!=='Succeeded'&&p.phase!=='Failed').length}`;$('metricNode').textContent=`${c?.nodes?.length||0} węzeł · ${(c?.deployments||[]).length} deploymentów`;
  const running=config.rps>0&&config.run_until*1000>Date.now();$('running').className='pill'+(running?' running':'');$('running').textContent=running?`${config.rps} req/s · ${Math.max(0,Math.ceil(config.run_until-Date.now()/1000))} s`:'Zatrzymany';
  if($('view-map').classList.contains('active'))renderGraph();if(!$('inspector').contains(document.activeElement))renderInspector();
  const events=state.events.filter(e=>e.trace&&e.kind!=='action'&&e.kind!=='span'&&e.kind!=='request-start').slice(0,30);markup('eventList',events.length?events.map(e=>`<button class="event" data-trace="${esc(e.trace)}"><time>${time(e.time)}</time><span>${esc(e.from)} → ${esc(e.to)}</span><span class="code ${e.status>=400?'bad':''}">${e.status||'evt'}</span><span class="duration">${e.duration_ms.toFixed(0)} ms</span></button>`).join(''):'<div class="empty">Uruchom generator lub dodaj zadanie do kolejki.</div>');
  $('eventList').querySelectorAll('[data-trace]').forEach(b=>b.onclick=()=>renderTrace(b.dataset.trace));
  markup('queueCounters',[['Oczekujące',queue.pending],['W trakcie / bez ACK',queue.in_flight],['Ponownie dostarczane',queue.redelivered],['Dead-letter queue',queue.dead_letters]].map(([label,value])=>`<div class="queue-count"><strong>${value}</strong><span>${label}</span></div>`).join(''));
  markup('taskTable',table(['Zadanie','Status','Próby','HTTP','Aktualizacja'],(queue.tasks||[]).map(t=>[`<button class="text-button" data-task-trace="${esc(t.trace||t.id)}">${esc(t.id)}</button>`,badge(t.state),t.attempt,t.http_status||'—',time(t.updated)])));
  $('taskTable').querySelectorAll('[data-task-trace]').forEach(b=>b.onclick=()=>{switchTab('map');renderTrace(b.dataset.taskTrace);});
  const jobs=c?.jobs||[];markup('jobTable',table(['Job','Aktywne','Sukcesy','Nieudane pody','Wynik'],jobs.map(j=>[esc(j.name),j.active,j.succeeded,j.failed,badge(j.succeeded?'completed':(j.conditions||[]).some(x=>x.type==='Failed'&&x.status==='True')?'failed':j.active?'processing':'pending')])));
  const cron=c?.cronjobs?.[0];$('cronState').textContent=cron?`${cron.suspended?'Wstrzymany':'Aktywny'} · ${cron.schedule} · ostatnie uruchomienie: ${time(cron.last_schedule)}`:'Oczekiwanie na CronJob';
  $('selfConfig').textContent=JSON.stringify({...state.self,scenario:config,discovery_at:c?.at,discovery_errors:c?.errors||[]},null,2);
  markup('nodeInfo',(c?.nodes||[]).map(n=>`<div class="node-summary"><strong>${esc(n.name)}</strong><small>${esc(n.version)} · CPU ${esc(n.capacity?.cpu)} · RAM ${esc(n.capacity?.memory)}<br>Ready: ${esc((n.conditions||[]).find(x=>x.type==='Ready')?.status||'Unknown')}</small></div>`).join('')+`<p class="muted">${esc(c?.usage_error||'Metryki CPU i RAM dostępne z Metrics Server.')}<br>Zakres obserwatora: węzły i zasoby namespace systems-lab. Bez dostępu do Secretów.</p>`);
  markup('serviceTable',table(['Usługa','Cluster IP','Porty','Selektor'],(c?.services||[]).map(s=>[esc(s.name),esc(s.ip),(s.ports||[]).map(p=>esc(p.port)+'/'+esc(p.protocol)).join(', '),`<span class="code">${esc(JSON.stringify(s.selector))}</span>`])));
  markup('kubeEvents',table(['Czas','Powód','Zasób','Wiadomość'],(c?.events||[]).slice(0,25).map(e=>[time(e.time),esc(e.reason),esc(e.object?.name),esc(e.message)])));
  window.labExperimentState?.(state);window.labResilienceState?.(state);window.labAutoscalingState?.(state);window.labPortfolioUpdate?.(state);window.labNetworkState?.(state);window.labLearningUpdate?.(state);window.labPerformanceState?.(state);$('lastUpdate').textContent=`Odczyt panelu ${time(state.at)} · discovery ${time(c?.at)}`;
}
async function refresh() {if(busy)return;busy=true;try{state=await request('/api/state');render();$('refreshStatus').textContent=`Odczyt ${time(state.at)} · co 3 s`;}catch(e){$('connection').textContent='Brak połączenia · ponawiam';$('connectionDot').className='status-dot';$('refreshStatus').textContent='Odczyt nieudany · ponawiam co 3 s';}finally{busy=false;}}
async function init() {try{token=(await request('/api/session')).token;await refresh();}catch(e){notify('Panel uruchamia się. Ponawiam połączenie.',true);setTimeout(init,3000);return;}setInterval(()=>{if(!document.hidden)refresh();},3000);}
document.addEventListener('visibilitychange',()=>{if(!document.hidden)refresh();});
switchTab(location.hash.slice(1)||'portfolio');
init();
