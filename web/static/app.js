(() => {
  window.autoRefresh = false;
  let playTimer = null;
  let fps = 15;

  const modal = () => document.getElementById('modal');


  window.closeModal = function () {
    const m = modal();
    if (!m) return;
    m.hidden = true;
    const content = document.getElementById('modal-content');
    if (content) content.innerHTML = '';
    stopPlay();
  };

  function stopPlay() {
    if (playTimer) {
      clearInterval(playTimer);
      playTimer = null;
      const b = document.getElementById('play');
      if (b) b.textContent = '▶';
    }
  }

  function frameMax() {
    const s = document.getElementById('scrub');
    return s ? +s.max : 0;
  }

  function setFrame(i) {
    const img = document.getElementById('frame-img');
    const scrub = document.getElementById('scrub');
    if (!img || !scrub) return;
    const max = frameMax();
    i = Math.max(0, Math.min(max, i));
    img.src = '/frame/' + img.dataset.job + '/' + i;
    scrub.value = i;
    const no = document.getElementById('frame-no');
    if (no) no.textContent = i + ' / ' + max;
    for (let k = 1; k <= 3; k++) {
      const nxt = i + k;
      if (nxt <= max) {
        const im = new Image();
        im.src = '/frame/' + img.dataset.job + '/' + nxt;
      }
    }
  }

  function togglePlay() {
    if (playTimer) {
      stopPlay();
      return;
    }
    const b = document.getElementById('play');
    if (b) b.textContent = '⏸';
    playTimer = setInterval(() => {
      const scrub = document.getElementById('scrub');
      if (!scrub) {
        stopPlay();
        return;
      }
      let i = +scrub.value + 1;
      if (i > frameMax()) i = 0;
      setFrame(i);
    }, 1000 / fps);
  }

  document.addEventListener('click', (e) => {
    const modeBtn = e.target.closest('#modes button');
    if (modeBtn) {
      const stage = document.getElementById('stage');
      if (stage) stage.dataset.mode = modeBtn.dataset.mode;
      document.querySelectorAll('#modes button').forEach((b) => b.classList.toggle('active', b === modeBtn));
      return;
    }
    if (e.target.closest('[data-open-modal]')) {
      const m = modal();
      if (m) m.hidden = false;
      return;
    }
    if (e.target.closest('#play')) {
      togglePlay();
      return;
    }
    if (e.target === modal() || e.target.closest('[onclick="closeModal()"]')) {
      closeModal();
    }
  });

  document.addEventListener('input', (e) => {
    if (e.target.id === 'scrub') setFrame(+e.target.value);
  });

  document.addEventListener('change', (e) => {
    if (e.target.id === 'fps') {
      fps = +e.target.value;
      if (playTimer) {
        stopPlay();
        togglePlay();
      }
    }
    if (e.target.id === 'autorefresh') {
      window.autoRefresh = e.target.checked;
    }
  });

  document.addEventListener('keydown', (e) => {
    const m = modal();
    if (m && !m.hidden) {
      if (e.key === 'Escape') {
        closeModal();
        return;
      }
      const scrub = document.getElementById('scrub');
      if (!scrub) return;
      if (e.key === 'ArrowRight') {
        setFrame(+scrub.value + 1);
        e.preventDefault();
      } else if (e.key === 'ArrowLeft') {
        setFrame(+scrub.value - 1);
        e.preventDefault();
      } else if (e.key === ' ') {
        togglePlay();
        e.preventDefault();
      }
    }
  });

  const drag = { active: false, el: null };
  function moveDivider(c, clientX) {
    const r = c.getBoundingClientRect();
    let pct = ((clientX - r.left) / r.width) * 100;
    pct = Math.max(0, Math.min(100, pct));
    c.style.setProperty('--pos', pct + '%');
  }
  document.addEventListener('pointerdown', (e) => {
    const c = e.target.closest('.compare');
    if (!c) return;
    drag.active = true;
    drag.el = c;
    moveDivider(c, e.clientX);
    e.preventDefault();
  });
  window.addEventListener('pointermove', (e) => {
    if (drag.active && drag.el) moveDivider(drag.el, e.clientX);
  });
  window.addEventListener('pointerup', () => {
    drag.active = false;
    drag.el = null;
  });
})();
