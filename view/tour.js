// The tour player: which step is showing and whether autoplay runs.
// It owns no DOM and no clock of its own. show(index) applies a step and
// change(state) repaints the controls; the timer is injected so tests can
// drive it by hand.

export function createPlayer(steps, { show, change, setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
  const state = { index: -1, playing: false, ended: false, count: steps.length };
  let timer = null;

  const notify = () => change && change({ ...state });
  const stopTimer = () => {
    if (timer !== null) clearTimer(timer);
    timer = null;
  };
  const arm = () => {
    stopTimer();
    if (!state.playing || state.index < 0) return;
    const seconds = Number(steps[state.index].duration) || 0;
    timer = setTimer(onTimer, Math.max(0.5, seconds) * 1000);
  };
  function onTimer() {
    timer = null;
    if (state.index >= steps.length - 1) {
      state.playing = false;
      state.ended = true;
      notify();
      return;
    }
    go(state.index + 1);
  }
  function go(index) {
    if (!steps.length) return false;
    const next = Math.max(0, Math.min(steps.length - 1, index));
    if (next === state.index && state.index >= 0) {
      arm();
      return false;
    }
    state.index = next;
    state.ended = false;
    if (show) show(next, steps[next]);
    arm();
    notify();
    return true;
  }

  return {
    get state() {
      return { ...state };
    },
    start() {
      return go(0);
    },
    jump(index) {
      return go(index);
    },
    next() {
      if (state.index >= steps.length - 1) return false;
      return go(state.index + 1);
    },
    prev() {
      if (state.index <= 0) return false;
      return go(state.index - 1);
    },
    play() {
      if (!steps.length) return;
      if (state.ended || state.index < 0) {
        state.playing = true;
        state.ended = false;
        if (!go(0)) {
          arm();
          notify();
        }
        return;
      }
      state.playing = true;
      arm();
      notify();
    },
    pause() {
      state.playing = false;
      stopTimer();
      notify();
    },
    toggle() {
      if (state.playing) this.pause();
      else this.play();
    },
    stop() {
      state.playing = false;
      stopTimer();
    },
  };
}
