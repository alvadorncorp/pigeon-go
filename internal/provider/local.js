const inbox = document.querySelector('.v-c');
const picker = document.querySelector('.c-picker');
inbox.classList.add('js');

const reading = document.querySelector('.c-reading');
try {
  if (sessionStorage.getItem('pigeon-selection') === location.href) {
    sessionStorage.removeItem('pigeon-selection');
    reading.tabIndex = -1;
    reading.focus({ preventScroll: true });
    window.scrollTo(0, 0);
  }
  document.querySelectorAll('.c-message, .message-pager a').forEach(link => {
    link.addEventListener('click', () => sessionStorage.setItem('pigeon-selection', link.href));
  });
} catch {
  // Navigation still works when browser storage is unavailable.
}

picker.addEventListener('click', () => {
  const open = inbox.classList.toggle('is-list-open');
  picker.setAttribute('aria-expanded', String(open));
  picker.querySelector('span').textContent = open ? 'Fechar lista' : 'Escolher mensagem';
});

const tabs = [...document.querySelectorAll('.format-tabs a[role="tab"]')];
function selectFormat(tab, updateURL) {
  for (const other of tabs) {
    const selected = other === tab;
    other.setAttribute('aria-selected', String(selected));
    other.tabIndex = selected ? 0 : -1;
    document.getElementById(other.getAttribute('aria-controls')).hidden = !selected;
  }
  document.querySelector('.c-format-note').textContent = tab.dataset.format === 'html' ? 'Visualização isolada' : 'Conteúdo original';
  if (updateURL) history.replaceState(null, '', tab.href);
}
for (const tab of tabs) {
  tab.addEventListener('click', event => {
    event.preventDefault();
    selectFormat(tab, true);
  });
  tab.addEventListener('keydown', event => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const index = tabs.indexOf(tab);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
    selectFormat(tabs[next], true);
    tabs[next].focus();
  });
}
