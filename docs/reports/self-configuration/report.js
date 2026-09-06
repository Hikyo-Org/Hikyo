// Standalone report interactions. No network requests or product mutations.
const scenarios = [/* SCENARIOS */];
const root = document.documentElement;
const themeButton = document.getElementById('theme');
const preferredLight = window.matchMedia('(prefers-color-scheme: light)').matches;
root.dataset.theme = preferredLight ? 'light' : 'dark';
function updateThemeLabel() {
  themeButton.textContent = root.dataset.theme === 'dark' ? 'Use light theme' : 'Use dark theme';
}
updateThemeLabel();
themeButton.addEventListener('click', () => {
  root.dataset.theme = root.dataset.theme === 'dark' ? 'light' : 'dark';
  updateThemeLabel();
});

const search = document.getElementById('search');
const groupSelect = document.getElementById('group-filter');
const approvalSelect = document.getElementById('approval-filter');
const count = document.getElementById('result-count');
const decisions = [...document.querySelectorAll('.decision')];
const groups = [...document.querySelectorAll('.decision-group')];
const expandButton = document.getElementById('expand');
function updateExpandLabel() {
  const visible = decisions.filter((decision) => !decision.hidden);
  expandButton.textContent = visible.length && visible.every((decision) => decision.open) ? 'Collapse visible' : 'Expand visible';
  expandButton.disabled = visible.length === 0;
}
function filterDecisions() {
  const query = search.value.trim().toLowerCase();
  let matches = 0;
  for (const decision of decisions) {
    const visible = (!query || decision.textContent.toLowerCase().includes(query))
      && (!groupSelect.value || decision.dataset.group === groupSelect.value)
      && (!approvalSelect.value || decision.dataset.approval === approvalSelect.value);
    decision.hidden = !visible;
    if (visible) matches += 1;
  }
  for (const group of groups) {
    group.hidden = ![...group.querySelectorAll('.decision')].some((decision) => !decision.hidden);
  }
  count.textContent = matches + ' of ' + decisions.length + ' decisions';
  document.getElementById('empty').hidden = matches !== 0;
  updateExpandLabel();
}
search.addEventListener('input', filterDecisions);
groupSelect.addEventListener('change', filterDecisions);
approvalSelect.addEventListener('change', filterDecisions);
expandButton.addEventListener('click', () => {
  const visible = decisions.filter((decision) => !decision.hidden);
  const open = !visible.every((decision) => decision.open);
  for (const decision of visible) decision.open = open;
  updateExpandLabel();
});
for (const decision of decisions) decision.addEventListener('toggle', updateExpandLabel);
function revealHash() {
  const id = window.location.hash.slice(1);
  const decision = decisions.find((item) => item.id === id);
  if (!decision) return;
  search.value = '';
  groupSelect.value = '';
  approvalSelect.value = '';
  filterDecisions();
  decision.open = true;
  decision.scrollIntoView({block: 'start'});
}
window.addEventListener('hashchange', revealHash);
revealHash();

let selectedScenario = 0;
let selectedStep = 0;
const scenarioButtons = [...document.querySelectorAll('[data-scenario]')];
const previousButton = document.getElementById('previous-step');
const nextButton = document.getElementById('next-step');
function renderScenario() {
  const scenario = scenarios[selectedScenario];
  const step = scenario.steps[selectedStep];
  document.getElementById('scenario-title').textContent = scenario.title;
  document.getElementById('scenario-intro').textContent = scenario.intro;
  const outcome = document.getElementById('scenario-outcome');
  const phase = scenario.phases[selectedStep];
  outcome.textContent = phase;
  const tone = phase === 'Applied' ? 'good' : ['Partial', 'Pending'].includes(phase) ? 'warn' : ['Validation failed', 'Rejected'].includes(phase) ? 'bad' : '';
  outcome.className = 'outcome ' + tone;
  document.getElementById('desired-revision').textContent = step[1];
  document.getElementById('node-revisions').textContent = step[2];
  document.getElementById('step-title').textContent = step[0];
  document.getElementById('step-detail').textContent = step[3];
  document.getElementById('step-count').textContent = 'Step ' + (selectedStep + 1) + ' of ' + scenario.steps.length;
  const progress = document.getElementById('step-progress');
  progress.replaceChildren(...scenario.steps.map((_, index) => {
    const bar = document.createElement('span');
    bar.className = 'step-dot' + (index <= selectedStep ? ' active' : '');
    return bar;
  }));
  previousButton.disabled = selectedStep === 0;
  nextButton.disabled = selectedStep === scenario.steps.length - 1;
  scenarioButtons.forEach((button, index) => button.setAttribute('aria-pressed', String(index === selectedScenario)));
}
scenarioButtons.forEach((button, index) => button.addEventListener('click', () => {
  selectedScenario = index;
  selectedStep = 0;
  renderScenario();
}));
previousButton.addEventListener('click', () => {
  if (selectedStep > 0) selectedStep -= 1;
  renderScenario();
});
nextButton.addEventListener('click', () => {
  if (selectedStep < scenarios[selectedScenario].steps.length - 1) selectedStep += 1;
  renderScenario();
});
renderScenario();

let printedDetails = [];
window.addEventListener('beforeprint', () => {
  printedDetails = [...document.querySelectorAll('details:not(.license)')].map((detail) => ({detail, open: detail.open}));
  for (const {detail} of printedDetails) detail.open = true;
});
window.addEventListener('afterprint', () => {
  for (const {detail, open} of printedDetails) detail.open = open;
  printedDetails = [];
});
document.getElementById('print').addEventListener('click', () => window.print());

document.getElementById('download-html').href = window.location.href.split('#')[0];
const exportData = document.getElementById('report-data').textContent;
const dataURL = URL.createObjectURL(new Blob([exportData], {type: 'application/json'}));
document.getElementById('download-data').href = dataURL;
window.addEventListener('pagehide', (event) => {
  if (!event.persisted) URL.revokeObjectURL(dataURL);
});
