'use strict';

const root = document.documentElement;
const themeButton = document.querySelector('.theme-toggle');
const themeColor = document.getElementById('theme-color');

function syncThemeControl() {
  const light = root.dataset.theme === 'light';
  const label = light ? 'Switch to dark theme' : 'Switch to light theme';
  themeButton.setAttribute('aria-label', label);
  themeButton.title = label;
  themeColor.content = light ? '#e8ebdf' : '#161c1d';
}

themeButton.hidden = false;
syncThemeControl();
themeButton.addEventListener('click', () => {
  root.dataset.theme = root.dataset.theme === 'dark' ? 'light' : 'dark';
  syncThemeControl();
  try {
    localStorage.setItem('mcbe2friends-theme', root.dataset.theme);
  } catch {}
});

const menu = document.querySelector('.mobile-menu');
const menuButton = menu.querySelector('summary');
menu.addEventListener('click', (event) => {
  if (event.target.closest('a')) menu.open = false;
});
document.addEventListener('click', (event) => {
  if (menu.open && !menu.contains(event.target)) menu.open = false;
});
document.addEventListener('focusin', (event) => {
  if (menu.open && !menu.contains(event.target)) menu.open = false;
});
document.addEventListener('keydown', (event) => {
  if (menu.open && event.key === 'Escape') {
    menu.open = false;
    menuButton.focus();
  }
});

const hero = document.querySelector('.hero');
const rail = document.querySelector('.rail');
const scenes = Array.from(rail.querySelectorAll('button'));
const cover = document.getElementById('cover-image');
const caption = document.getElementById('cover-caption');

function showScene(button) {
  if (button.getAttribute('aria-pressed') === 'true') return;
  for (const scene of scenes) scene.setAttribute('aria-pressed', String(scene === button));
  const art = button.dataset;
  hero.dataset.scene = art.scene;
  cover.alt = art.alt;
  cover.src = 'assets/gallery/' + art.cover + '.webp';
  caption.textContent = art.caption;
}

rail.hidden = false;
for (const [index, button] of scenes.entries()) {
  button.addEventListener('click', () => showScene(button));
  button.addEventListener('keydown', (event) => {
    const last = scenes.length - 1;
    const targets = {
      ArrowUp: (index + last) % scenes.length,
      ArrowLeft: (index + last) % scenes.length,
      ArrowDown: (index + 1) % scenes.length,
      ArrowRight: (index + 1) % scenes.length,
      Home: 0,
      End: last
    };
    if (!Object.hasOwn(targets, event.key)) return;
    event.preventDefault();
    const next = scenes[targets[event.key]];
    next.focus();
    showScene(next);
  });
}

const walkthrough = document.getElementById('walkthrough');
const guideFilm = document.getElementById('guide-film');
const chapterLinks = Array.from(walkthrough.querySelectorAll('.film-chapters a'));
const chapterTimes = chapterLinks.map(link => Number(link.hash.slice(3)));
let pendingChapter;

function openLinkedWalkthrough() {
  if (location.hash === '#walkthrough') walkthrough.open = true;
}
openLinkedWalkthrough();
window.addEventListener('hashchange', openLinkedWalkthrough);

function playGuide(time) {
  walkthrough.open = true;
  if (time !== undefined) {
    if (guideFilm.readyState >= HTMLMediaElement.HAVE_METADATA) guideFilm.currentTime = time;
    else pendingChapter = time;
  } else if (guideFilm.ended) {
    guideFilm.currentTime = 0;
  }
  guideFilm.play().catch(() => {
    // Browsers that require another gesture keep their native play control.
  });
}
guideFilm.addEventListener('loadedmetadata', () => {
  if (pendingChapter !== undefined) {
    guideFilm.currentTime = pendingChapter;
    pendingChapter = undefined;
  }
});
guideFilm.addEventListener('timeupdate', () => {
  let current = 0;
  for (let index = 1; index < chapterTimes.length; index++) {
    if (guideFilm.currentTime >= chapterTimes[index]) current = index;
  }
  chapterLinks.forEach((link, index) => {
    if (index === current) link.setAttribute('aria-current', 'time');
    else link.removeAttribute('aria-current');
  });
});
function normalClick(event) {
  return event.button === 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey;
}
for (const link of document.querySelectorAll('a[href="#walkthrough"]')) {
  link.addEventListener('click', event => {
    if (!normalClick(event)) return;
    if (link.id === 'watch-walkthrough') playGuide();
    else walkthrough.open = true;
  });
}
chapterLinks.forEach((link, index) => link.addEventListener('click', event => {
  if (!normalClick(event)) return;
  event.preventDefault();
  playGuide(chapterTimes[index]);
}));
walkthrough.addEventListener('toggle', () => {
  if (!walkthrough.open) guideFilm.pause();
});
