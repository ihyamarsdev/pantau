/* CodeMirror 5 mode for HCL (Terraform .tf/.tfvars/.hcl). Token classes reuse the vscode-dark theme in index.html. */
(function () {
  'use strict';
  if (typeof CodeMirror === 'undefined') return;
  const KEYWORDS = new Set(['resource', 'data', 'variable', 'output', 'locals', 'module', 'provider', 'terraform', 'for_each', 'count', 'dynamic', 'for', 'in', 'if']);
  const REFS = new Set(['var', 'local', 'each', 'path', 'self']);
  const ATOMS = new Set(['true', 'false', 'null']);

  // Body of a quoted string; `${` / `%{` open an interpolation frame, `$${` is an escape.
  function strTok(stream, state) {
    const start = stream.pos;
    while (!stream.eol()) {
      const c = stream.peek();
      if (c === '"') {
        if (stream.pos > start) return 'string';
        stream.next(); state.ctx.pop(); return 'string';
      }
      if (c === '\\') { stream.next(); stream.next(); continue; }
      if (c === '$' || c === '%') {
        if (stream.match(/^[$%]{2}\{/)) continue;
        if (stream.match(/^[$%]\{/, false)) {
          if (stream.pos > start) return 'string';
          stream.pos += 2; state.ctx.push({ d: 0 }); return 'variable-2';
        }
      }
      stream.next();
    }
    return 'string';
  }

  function codeTok(stream, state) {
    if (stream.eatSpace()) return null;
    const first = state.first; state.first = false;
    const top = state.ctx[state.ctx.length - 1];
    if (stream.match(/^(#|\/\/)/)) { stream.skipToEnd(); return 'comment'; }
    if (stream.match('/*')) { state.block = true; return blockTok(stream, state); }
    if (stream.match(/^<<-?([A-Za-z_]\w*)/)) { state.heredoc = stream.current().replace(/^<<-?/, ''); return 'string-2'; }
    if (stream.peek() === '"') {
      if (state.hdr) { stream.match(/^"(?:[^"\\]|\\.)*"?/); return 'def'; }
      stream.next(); state.ctx.push({ str: true }); return 'string';
    }
    if (stream.match(/^\d+(\.\d+)?([eE][+-]?\d+)?/)) return 'number';
    if (stream.match(/^[A-Za-z_][\w-]*/)) {
      const w = stream.current();
      if (ATOMS.has(w)) return 'atom';
      if (!state.ctx.length && first && stream.match(/^\s*(?:"|[A-Za-z_][\w-]*\s*["{]|\{)/, false)) { state.hdr = true; return 'keyword'; }
      if (state.hdr) return 'def';
      if (KEYWORDS.has(w)) return 'keyword';
      if (stream.match(/^\s*=(?![=>])/, false)) return 'property';
      if (REFS.has(w)) return 'variable-2';
      if (stream.peek() === '(') return 'variable-3';
      return 'variable';
    }
    const ch = stream.next();
    if (ch === '{') { state.hdr = false; if (top) top.d++; return null; }
    if (ch === '}' && top) {
      if (top.d === 0) { state.ctx.pop(); return 'variable-2'; }
      top.d--; return null;
    }
    if ('=!<>+-*/%&|?:'.includes(ch)) { stream.match(/^[=>&|]/); return 'operator'; }
    return null;
  }

  function blockTok(stream, state) {
    while (!stream.eol()) if (stream.match('*/')) { state.block = false; break; } else stream.next();
    return 'comment';
  }

  CodeMirror.defineMode('hcl', function () {
    return {
      lineComment: '#', blockCommentStart: '/*', blockCommentEnd: '*/',
      startState: () => ({ ctx: [], block: false, heredoc: null, first: true, hdr: false }),
      copyState: s => Object.assign({}, s, { ctx: s.ctx.map(f => Object.assign({}, f)) }),
      token(stream, state) {
        if (state.heredoc) {
          if (stream.sol() && stream.match(new RegExp('^\\s*' + state.heredoc.replace(/\W/g, '\\$&') + '\\s*$'))) state.heredoc = null;
          else stream.skipToEnd();
          return 'string-2';
        }
        if (state.block) return blockTok(stream, state);
        // Strings are single-line: any unfinished state is reset at the line start.
        if (stream.sol()) { state.ctx = []; state.first = true; state.hdr = false; }
        const top = state.ctx[state.ctx.length - 1];
        return top && top.str ? strTok(stream, state) : codeTok(stream, state);
      },
    };
  });
  CodeMirror.defineMIME('text/x-hcl', 'hcl');
})();
