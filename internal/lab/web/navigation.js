(() => {
  const rail=$('navigationRail'),toggle=$('menuToggle'),backdrop=$('menuBackdrop'),shell=document.querySelector('.shell');
  const mobile=matchMedia('(max-width: 850px)');
  const get=(key,fallback)=>{try{return localStorage.getItem(key)??fallback;}catch{return fallback;}};
  const save=(key,value)=>{try{localStorage.setItem(key,value);}catch{/* Preferences are optional. */}};
  let compact=get('systems-lab.nav.compact',innerWidth<1150?'1':'0')==='1',open=false;
  const descriptions={
    portfolio:'Trzy krótkie demonstracje. Wybierz problem i przejdź przez jego diagnozę.',
    learn:'Instrukcje krok po kroku i słownik pojęć — dobry punkt startu.',
    performance:'Najpierw sprawdź objawy, potem etap pracy lub ślad pojedynczego żądania.',
    map:'Zobacz połączenia usług. Kliknij usługę, aby sprawdzić jej pody i logi.',
    network:'Porównaj tempo żądań i ilość rzeczywiście przesyłanych danych aplikacji.',
    experiments:'Zaplanuj dwie próby, porównaj wyniki i sprawdź ich ślady.',
    resilience:'Wywołaj kontrolowaną awarię, sprawdź ochronę lub włącz skalowanie CPU.',
    queue:'Wstrzymaj odbiór, dodaj zadania i zobacz, jak kolejka wraca do pracy.',
    jobs:'Uruchom pojedyncze zadanie albo ustaw harmonogram i obserwuj wykonania.',
    cluster:'Sprawdź stan środowiska. Szczegóły konfiguracji są w osobnej zakładce.'
  };
  function menu({restore=false}={}) {
    const drawer=mobile.matches;
    document.documentElement.classList.toggle('nav-compact',compact&&!drawer);
    document.documentElement.classList.toggle('menu-open',open&&drawer);
    rail.inert=drawer&&!open;
    shell.inert=drawer&&open;
    backdrop.hidden=!(drawer&&open);
    if(drawer&&!open)rail.setAttribute('aria-hidden','true');else rail.removeAttribute('aria-hidden');
    if(drawer&&open){rail.setAttribute('role','dialog');rail.setAttribute('aria-modal','true');}
    else{rail.removeAttribute('role');rail.removeAttribute('aria-modal');}
    toggle.setAttribute('aria-expanded',String(drawer?open:!compact));
    toggle.setAttribute('aria-label',drawer?(open?'Zamknij menu':'Otwórz menu'):(compact?'Rozwiń menu':'Zwiń menu'));
    toggle.title=toggle.getAttribute('aria-label');
    toggle.textContent=drawer||compact?'☰':'‹';
    if(restore)toggle.focus({preventScroll:true});
  }
  function closeMenu(){if(!open)return;open=false;menu({restore:true});}
  toggle.onclick=()=>{
    if(mobile.matches){open=!open;menu();if(open)(rail.querySelector('.nav[aria-current="page"]')||$('navClose')).focus();}
    else{compact=!compact;save('systems-lab.nav.compact',compact?'1':'0');menu();}
  };
  $('navClose').onclick=closeMenu;backdrop.onclick=closeMenu;
  document.querySelector('.brand').onclick=e=>{e.preventDefault();switchTab('portfolio');};
  mobile.addEventListener('change',()=>{const needsFocus=rail.contains(document.activeElement)||shell.inert;open=false;menu({restore:needsFocus});});
  document.addEventListener('keydown',e=>{
    if(!mobile.matches||!open)return;
    if(e.key==='Escape'){e.preventDefault();closeMenu();return;}
    if(e.key!=='Tab')return;
    const items=[...rail.querySelectorAll('button:not(:disabled),a[href]')].filter(e=>e.getClientRects().length);
    const first=items[0],last=items.at(-1);
    if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus();}
    else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}
  });
  const panels=new Map();
  document.querySelectorAll('.section-tabs').forEach(list=>{
    const buttons=[...list.querySelectorAll('[data-panel-target]')],view=buttons[0].dataset.panelView;
    panels.set(view,{buttons,list});
    buttons.forEach(button=>button.addEventListener('click',()=>show(view,button.dataset.panelTarget)));
    list.addEventListener('keydown',e=>{
      if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key)||!buttons.includes(e.target))return;
      e.preventDefault();let index=buttons.indexOf(e.target);
      index=e.key==='Home'?0:e.key==='End'?buttons.length-1:(index+(e.key==='ArrowRight'?1:-1)+buttons.length)%buttons.length;
      show(view,buttons[index].dataset.panelTarget,{focus:true});
    });
  });
  function show(view,name,{focus=false}={}) {
    const group=panels.get(view);if(!group)return;
    const active=group.buttons.find(b=>b.dataset.panelTarget===name)||group.buttons[0];
    group.buttons.forEach(button=>{
      const selected=button===active;button.setAttribute('aria-selected',String(selected));button.tabIndex=selected?0:-1;
      const panel=$(button.getAttribute('aria-controls'));panel.hidden=!selected;
    });
    save('systems-lab.panel.'+view,active.dataset.panelTarget);
    if(view==='performance')$('performanceFilters').hidden=!['overview','functions'].includes(active.dataset.panelTarget);
    if(focus)active.focus();
    window.dispatchEvent(new CustomEvent('lab-panel',{detail:{view,panel:active.dataset.panelTarget}}));
  }
  window.labPanels={show};
  for(const [view,group] of panels)show(view,get('systems-lab.panel.'+view,group.buttons[0].dataset.panelTarget));
  function viewChanged(view){
    document.body.dataset.view=view;$('viewDescription').textContent=descriptions[view]||'';
    closeMenu();
  }
  window.addEventListener('lab-tab',e=>viewChanged(e.detail));
  viewChanged(document.querySelector('.view.active').id.slice(5));menu();
})();
