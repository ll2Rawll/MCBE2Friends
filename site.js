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
const chapters = Array.from(walkthrough.querySelectorAll('.film-chapters a'), link => ({
  link,
  hash: '#' + link.id,
  time: Number(link.hash.slice(3))
}));
let pendingTime;

function markChapter(time) {
  const active = chapters.reduce((last, chapter) => time + 0.001 >= chapter.time ? chapter : last, chapters[0]);
  for (const chapter of chapters) {
    if (chapter === active) chapter.link.setAttribute('aria-current', 'time');
    else chapter.link.removeAttribute('aria-current');
  }
}

function seekChapter(chapter) {
  pendingTime = chapter.time;
  if (guideFilm.readyState >= HTMLMediaElement.HAVE_METADATA) {
    guideFilm.currentTime = pendingTime;
    pendingTime = undefined;
  } else if (guideFilm.preload !== 'metadata') {
    guideFilm.preload = 'metadata';
    if (guideFilm.networkState !== HTMLMediaElement.NETWORK_LOADING) guideFilm.load();
  }
  markChapter(chapter.time);
}

function revealWalkthrough(chapter, play = false) {
  walkthrough.open = true;
  if (chapter) seekChapter(chapter);
  else if (play && guideFilm.ended) guideFilm.currentTime = 0;

  if (play) {
    guideFilm.focus({ preventScroll: true });
    guideFilm.play().catch(() => {
      // Native controls remain available when the browser requires another gesture.
    });
  }
  requestAnimationFrame(() => walkthrough.scrollIntoView({ block: 'start' }));
}

function followFragment() {
  const chapter = chapters.find(item => item.hash === location.hash);
  if (chapter || location.hash === '#walkthrough') revealWalkthrough(chapter);
}

guideFilm.addEventListener('loadedmetadata', () => {
  if (pendingTime !== undefined) {
    guideFilm.currentTime = pendingTime;
    markChapter(pendingTime);
    pendingTime = undefined;
  }
});
guideFilm.addEventListener('timeupdate', () => {
  if (pendingTime === undefined) markChapter(guideFilm.currentTime);
});
walkthrough.addEventListener('toggle', () => {
  if (!walkthrough.open) guideFilm.pause();
});

document.addEventListener('click', event => {
  if (event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  const link = event.target.closest('a');
  if (!link) return;
  const href = link.getAttribute('href');
  const chapter = chapters.find(item => item.link === link || item.hash === href);
  if (!chapter && href !== '#walkthrough') return;

  event.preventDefault();
  const hash = chapter ? chapter.hash : '#walkthrough';
  if (location.hash !== hash) history.pushState(null, '', hash);
  revealWalkthrough(chapter, Boolean(chapter) || link.id === 'watch-walkthrough');
});

window.addEventListener('hashchange', followFragment);
window.addEventListener('pageshow', followFragment);
markChapter(0);
