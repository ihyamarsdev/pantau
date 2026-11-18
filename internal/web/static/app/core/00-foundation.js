/* Foundation: SVG icons, i18n dictionaries, and shared host state.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';


function svgIcon(name, cls) { return '<svg class="icon' + (cls ? ' ' + cls : '') + '" aria-hidden="true"><use href="#i-' + name + '"/></svg>'; }


// Append many nodes in one DOM write to avoid layout thrashing in live lists.
function batchAppend(parent, nodes) {
  const frag = document.createDocumentFragment();
  for (const n of nodes) frag.appendChild(n);
  parent.appendChild(frag);
}

// Coalesce repeated render requests into one pass per animation frame.
function rafBatch(fn) {
  if (rafBatch._queued) return;
  rafBatch._queued = true;
  requestAnimationFrame(() => { rafBatch._queued = false; fn(); });
}

// Trailing-edge debounce for search/filter inputs.
function debounce(fn, ms) {
  let timer = null;
  return function (...args) {
    clearTimeout(timer);
    timer = setTimeout(() => fn.apply(this, args), ms);
  };
}

export { svgIcon, batchAppend, rafBatch, debounce };
