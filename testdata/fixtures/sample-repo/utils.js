// Utility functions

function debounce(fn, delay) {
  // TODO: Add cancel support
  let timer;
  return function (...args) {
    clearTimeout(timer);
    timer = setTimeout(() => fn.apply(this, args), delay);
  };
}

/* FIXME: This doesn't handle edge cases with Unicode characters */
function truncate(str, len) {
  return str.slice(0, len);
}

// XXX: Remove this before release
function debugLog(msg) {
  console.log("[DEBUG]", msg);
}
