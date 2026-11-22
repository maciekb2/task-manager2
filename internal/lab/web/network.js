(() => {
  let latest=null,pending=false,current=null;
  const bytes=n=>n>=1048576?(n/1048576).toFixed(2)+' MiB':n>=1024?(n/1024).toFixed(2)+' KiB':Math.round(n)+' B';
  const rate=n=>bytes(n)+'/s';
  const decimal=n=>Number(n||0).toLocaleString('pl-PL',{maximumFractionDigits:2});
  const protocol=e=>e.protocol==='http-response'?'HTTP · odpowiedź':'JetStream · wiadomość';
  function config(s) {
    current=s;const c=s.config,running=c.rps>0&&c.run_until*1000>Date.now();
    $('networkConfig').textContent=running?`Generator: zadane ${c.rps} req/s, nominalnie co ${decimal(1000/c.rps)} ms; maks. ${c.concurrency} równocześnie. Odpowiedź: JSON + ${bytes(c.payload_bytes||0)} dodatkowych danych. Worker: ${c.delay_ms} ms opóźnienia + ${c.cpu_ms} ms pracy CPU. To konfiguracja, pomiary są poniżej.`:'Generator zatrzymany. Możesz uruchomić eksperyment w sterowaniu; Joby i kolejka także mogą generować ruch. Liczniki pokazują jeszcze poprzednie zakończone transfery w oknie 30 s.';
  }
  function chart(points) {
    const W=800,H=190,L=48,R=12,T=12,B=32,scale=Math.max(1,...points.flatMap(p=>[p.http_bytes_per_second/1024,p.queue_bytes_per_second/1024]))*1.1;
    if(!points.length){markup('networkChart','<p class="empty">Pierwsza próbka pojawi się po zamknięciu okna 5 s.</p>');return;}
    const start=Date.parse(points[0]?.at),end=Date.parse(points.at(-1)?.at);
    const x=i=>L+i/Math.max(1,points.length-1)*(W-L-R),y=n=>H-B-n/scale*(H-T-B);
    let svg=`<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Transfer danych aplikacji w KiB na sekundę">`;
    for(let i=0;i<4;i++){const n=scale*i/3;svg+=`<line x1="${L}" y1="${y(n)}" x2="${W-R}" y2="${y(n)}" stroke="#2b3845"/><text x="${L-5}" y="${y(n)+3}" text-anchor="end" class="axis-label">${decimal(n)}</text>`;}
    for(const [key,color] of [['http_bytes_per_second','#6fe5bd'],['queue_bytes_per_second','#f0bc6a']])svg+=`<path d="${points.map((p,i)=>`${i?'L':'M'}${x(i).toFixed(1)},${y(p[key]/1024).toFixed(1)}`).join(' ')}" fill="none" stroke="${color}" stroke-width="2"/>`;
    svg+=`<text x="${L}" y="${H-6}" class="axis-label">${time(start)}</text><text x="${W-R}" y="${H-6}" text-anchor="end" class="axis-label">${time(end)}</text></svg><div class="chart-legend"><span><i style="background:var(--mint)"></i>Body odpowiedzi HTTP · wszystkie odcinki</span><span><i style="background:var(--amber)"></i>Payload JetStream · publikacja i dostarczenie</span></div>`;
    const template=document.createElement('template');template.innerHTML=svg;patchSVG($('networkChart'),template.content);
  }
  function render() {
    if(!latest)return;const s=latest.summary,recent=latest.recent,edges=s.edges||[];
    const cards=[['TERAZ / OKNO 5 S',rate(recent.bytes_per_second),'Suma mierzonych odcinków'],['ŚREDNIA / OKNO 30 S',rate(s.bytes_per_second),(s.bytes_per_second*8/1e6).toFixed(3)+' Mbit/s danych aplikacji'],['HTTP / OKNO 30 S',rate(s.http_bytes_per_second),'Odpowiedzi · bez nagłówków'],['JETSTREAM / OKNO 30 S',rate(s.queue_bytes_per_second),'Wiadomości · bez ACK/NAK']];
    markup('networkMetrics',cards.map(([label,value,note])=>`<article><span>${esc(label)}</span><strong>${esc(value)}</strong><small>${esc(note)}</small></article>`).join(''));
    $('networkFreshness').textContent=`Odczyt ${time(latest.at)} · koniec okna ${time(s.window_end)} · co 5 s${Date.now()-Date.parse(latest.observed_since)<30000?' · rozgrzewanie okna 30 s':''}`;
    chart(latest.series||[]);
    const max=Math.max(1,...edges.map(e=>e.bytes_per_second));
    markup('networkFlows',edges.length?edges.map(e=>`<div class="network-flow"><div><span class="network-endpoint">${esc(titles[e.source]||e.source)}</span><small>${esc(protocol(e))}</small></div><div class="network-link"><span>${esc(rate(e.bytes_per_second))}</span><div class="network-track"><i style="width:${Math.max(2,100*e.bytes_per_second/max)}%" class="${e.protocol==='nats-message'?'queue':''}"></i></div><b aria-hidden="true">→</b></div><div><span class="network-endpoint">${esc(titles[e.target]||e.target)}</span><small>${decimal(e.per_second)} ${e.protocol==='http-response'?'odp.':'wiad.'}/s</small></div></div>`).join(''):'<p class="empty">Jeszcze brak zmierzonych transferów w zakończonym oknie. Uruchom ruch i poczekaj do 5 s na zamknięcie pierwszej próbki.</p>');
    markup('networkTable',table(['Dane płyną','Typ','Zakończenia/s','Średnio / transfer','Śr. odstęp','Transfer/s','Dane / 30 s'],edges.map(e=>[`${esc(e.source)} → ${esc(e.target)}`,esc(protocol(e)),decimal(e.per_second),bytes(e.mean_bytes),e.mean_interval_ms===null?'—':decimal(e.mean_interval_ms)+' ms',rate(e.bytes_per_second),bytes(e.bytes)])));
  }
  async function load() {
    if(pending)return;pending=true;
    try{latest=await request('/api/network');render();}catch(e){$('networkFreshness').textContent='Nieaktualne dane · '+e.message;}finally{pending=false;}
  }
  window.labNetworkState=config;
  window.addEventListener('lab-tab',e=>{if(e.detail==='network'){if(current)config(current);load();}});
  document.addEventListener('visibilitychange',()=>{if(!document.hidden&&$('view-network').classList.contains('active'))load();});
  setInterval(()=>{if(!document.hidden&&$('view-network').classList.contains('active'))load();},5000);
})();
