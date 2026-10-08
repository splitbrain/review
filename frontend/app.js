// Code Review Tool - Main Application
(function() {
  'use strict';

  const state = {
    tree: [],
    openDirs: {},
    currentFile: null,
    language: '',
    annotations: {},      // end line → {comment, startLine, endLine, outdated} for current file
    allAnnotations: {},   // path → {end line → {comment, startLine, endLine, outdated}} for all files
    diffLines: {},        // line → "added"|"modified" for current file
    diffHunks: [],        // [{startLine, endLine, diff}] for current file
    diffDeletions: [],    // [{afterLine, hunkIndex}] for current file
    totalLines: 0,        // number of lines in the current file
    scrollPositions: {},  // path → scrollTop of the code view when the file was left
    editingLine: null,    // end line of the comment being edited (the annotation's key)
    selectionStart: null, // first line of the range being selected/edited
    selectionEnd: null,   // last line of the range being selected/edited
    anchorLine: null,     // line a shift-click extends the selection from
    gitStatuses: {},
    wsConnected: false,
  };

  let ws = null;
  let wsReconnectDelay = 1000;
  let shuttingDown = false;

  // DOM references
  let treePane, treeContainer, codeContent, codeHeader, commentList, commentEditor,
      editorTextarea, editorLineLabel, statusCommentCount, statusMdPath,
      statusBase, wsIndicator, toastContainer;

  // Initialize
  document.addEventListener('DOMContentLoaded', () => {
    treePane = document.querySelector('.file-tree');
    treeContainer = document.getElementById('tree-container');
    codeContent = document.getElementById('code-content');
    codeHeader = document.getElementById('code-header');
    commentList = document.getElementById('comment-list');
    commentEditor = document.getElementById('comment-editor');
    editorTextarea = document.getElementById('editor-textarea');
    editorLineLabel = document.getElementById('editor-line-label');
    statusCommentCount = document.getElementById('status-comment-count');
    statusMdPath = document.getElementById('status-md-path');
    statusBase = document.getElementById('status-base');
    wsIndicator = document.getElementById('ws-indicator');
    toastContainer = document.getElementById('toast-container');

    attachCodeViewHandlers();

    loadConfig();
    loadTree();
    loadAllAnnotations();
    connectWebSocket();

    // Reposition scrollbar markers when code-content resizes
    new ResizeObserver(() => {
      const strip = document.querySelector('.scrollbar-markers');
      if (strip) positionScrollbarStrip(strip);
    }).observe(codeContent);
  });

  // API helpers
  async function api(method, path, body) {
    const opts = { method, headers: {} };
    if (body) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || 'Request failed');
    }
    return res.json();
  }

  // WebSocket connection
  function connectWebSocket() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + location.host + '/ws');

    ws.onopen = () => {
      state.wsConnected = true;
      wsReconnectDelay = 1000;
      updateWsIndicator();
    };

    ws.onclose = () => {
      // The server said it was going away, so there is nothing to reconnect to
      if (shuttingDown) return;

      state.wsConnected = false;
      updateWsIndicator();
      // Reconnect with exponential backoff
      setTimeout(() => {
        wsReconnectDelay = Math.min(wsReconnectDelay * 2, 30000);
        connectWebSocket();
      }, wsReconnectDelay);
    };

    ws.onerror = () => {
      ws.close();
    };

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        handleWsMessage(msg);
      } catch (e) {
        console.error('Failed to parse WebSocket message:', e);
      }
    };
  }

  function handleWsMessage(msg) {
    switch (msg.type) {
      case 'file-changed':
        // Update allAnnotations for this file
        if (msg.annotations) {
          state.allAnnotations[msg.path] = msg.annotations;
        }
        // The file being viewed reloads, which is feedback enough. Any other
        // file only shows up as a notice.
        if (state.currentFile === msg.path) {
          refreshCurrentFile(msg.annotations);
        } else {
          showToast('File changed: ' + msg.path);
        }
        updateCommentCount();
        renderTree();
        loadTreeSoon(); // the change may show up as a new git status
        break;

      case 'review-deleted':
        showToast('REVIEW.md was deleted — all annotations lost', true);
        state.allAnnotations = {};
        state.annotations = {};
        updateCommentCount();
        renderCommentList();
        renderTree();
        if (state.currentFile) {
          refreshCurrentFile();
        }
        break;

      case 'source-changed':
        // Source file changed on disk — reload if currently viewing it
        if (state.currentFile === msg.path) {
          refreshCurrentFile();
        }
        loadTreeSoon(); // the change may show up as a new git status
        break;

      case 'tree-changed':
        // A file appeared or went away
        loadTreeSoon();
        break;

      case 'review-reloaded': {
        showToast('REVIEW.md reloaded from disk');
        let current;
        if (msg.allAnnotations) {
          state.allAnnotations = msg.allAnnotations;
          current = msg.allAnnotations[state.currentFile] || {};
        } else {
          loadAllAnnotations();
        }
        if (state.currentFile) {
          refreshCurrentFile(current);
        }
        updateCommentCount();
        renderTree();
        break;
      }

      case 'server-shutdown':
        // Server is shutting down — close the tab
        shuttingDown = true;
        document.title = 'Review — closed';
        window.close();
        // Fallback if window.close() is blocked by browser policy
        document.body.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;height:100vh;color:var(--rv-muted)"><p>Server stopped. You can close this tab.</p></div>';
        break;
    }
  }

  function updateWsIndicator() {
    if (!wsIndicator) return;
    wsIndicator.className = 'ws-indicator ' + (state.wsConnected ? 'connected' : 'disconnected');
    wsIndicator.title = state.wsConnected ? 'Live updates connected' : 'Live updates disconnected';
  }

  function showToast(message, isError) {
    if (!toastContainer) return;
    const toast = document.createElement('div');
    toast.className = 'toast' + (isError ? ' toast-error' : '');
    toast.textContent = message;
    toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.classList.add('toast-fade');
      setTimeout(() => toast.remove(), 300);
    }, 4000);
  }

  // Load review settings and show the compare base if one is set
  async function loadConfig() {
    try {
      const config = await api('GET', '/api/config');
      if (config.base && statusBase) {
        statusBase.textContent = 'compared to ' + config.base;
        statusBase.title = 'Changes are highlighted relative to ' + config.base;
        statusBase.style.display = '';
      }
    } catch (e) {
      console.error('Failed to load config:', e);
    }
  }

  // Load the file tree together with the git status it is coloured by, so the
  // two are always drawn from the same view of the project.
  async function loadTree() {
    try {
      const [tree, gitStatuses] = await Promise.all([
        api('GET', '/api/tree'),
        api('GET', '/api/git-status'),
      ]);
      state.tree = tree;
      state.gitStatuses = gitStatuses;
      renderTree();
    } catch (e) {
      console.error('Failed to load tree:', e);
    }
  }

  // Reload the tree after a change on disk, debounced so that a burst of
  // changes does not run git once per file.
  let treeReloadTimer = null;
  function loadTreeSoon() {
    if (treeReloadTimer) clearTimeout(treeReloadTimer);
    treeReloadTimer = setTimeout(loadTree, 500);
  }

  // Load all annotations (for gutter dots and comment counts)
  async function loadAllAnnotations() {
    try {
      state.allAnnotations = await api('GET', '/api/annotations');
      updateCommentCount();
    } catch (e) {
      console.error('Failed to load annotations:', e);
    }
  }

  // Get git status for a file path
  function getGitStatus(path) {
    return state.gitStatuses[path] || '';
  }

  // How much a git status stands out, used to pick what a directory shows
  const statusPriority = { conflict: 5, modified: 4, deleted: 4, untracked: 3, added: 2, staged: 1 };

  // What a directory row shows, by directory path. Collected in one pass per
  // render rather than walking every subtree again for each row.
  let dirSummaries = new Map();

  // summarize walks a subtree once, recording for every directory the most
  // important git status below it and whether it holds any comments.
  function summarize(entry) {
    if (!entry.isDir) {
      const anns = state.allAnnotations[entry.path];
      return { status: getGitStatus(entry.path), comments: !!(anns && Object.keys(anns).length > 0) };
    }

    let status = '';
    let comments = false;
    for (const child of entry.children || []) {
      const childSummary = summarize(child);
      if ((statusPriority[childSummary.status] || 0) > (statusPriority[status] || 0)) {
        status = childSummary.status;
      }
      comments = comments || childSummary.comments;
    }

    const summary = { status, comments };
    dirSummaries.set(entry.path, summary);
    return summary;
  }

  // The sprite the tree's icons come from
  const icons = '/static/assets/icons.svg';

  // The row of each rendered path, so that a single row can be reached without
  // searching the tree for it, and the row marked as the open file.
  let treeRows = new Map();
  let activeRow = null;

  // Render file tree
  function renderTree() {
    dirSummaries = new Map();
    state.tree.forEach(summarize);

    const scrollTop = treePane.scrollTop;
    treeRows = new Map();
    activeRow = null;
    treeContainer.innerHTML = '';
    renderTreeLevel(state.tree, treeContainer, 0);
    treePane.scrollTop = scrollTop;
  }

  // Move the marking for the open file, which is all that opening one changes
  // about the tree.
  function setActiveFile(path) {
    if (activeRow) activeRow.classList.remove('active');
    activeRow = treeRows.get(path) || null;
    if (activeRow) activeRow.classList.add('active');
  }

  function renderTreeLevel(entries, container, depth) {
    entries.forEach(entry => {
      const item = document.createElement('div');
      item.className = 'tree-item';
      item.style.paddingLeft = (0.5 + depth * 1) + 'rem';
      treeRows.set(entry.path, item);

      if (entry.isDir) {
        const isOpen = state.openDirs[entry.path] || false;
        const summary = dirSummaries.get(entry.path) || { status: '', comments: false };
        if (summary.status) item.classList.add('git-' + summary.status);

        item.innerHTML =
          `<svg class="chevron${isOpen ? ' open' : ''}"><use href="${icons}#icon-chevron-right"/></svg>` +
          `<svg><use href="${icons}#icon-folder${isOpen ? '-open' : ''}"/></svg>` +
          `<span>${escapeHtml(entry.name)}</span>` +
          (summary.comments ? '<span class="comment-dot"></span>' : '');

        item.addEventListener('click', () => {
          state.openDirs[entry.path] = !state.openDirs[entry.path];
          renderTree();
        });

        container.appendChild(item);

        if (entry.children && isOpen) {
          const childContainer = document.createElement('div');
          childContainer.className = 'tree-children open';
          renderTreeLevel(entry.children, childContainer, depth + 1);
          container.appendChild(childContainer);
        }
      } else {
        const fileAnns = state.allAnnotations[entry.path];
        const hasComments = fileAnns && Object.keys(fileAnns).length > 0;
        const hasOutdated = hasComments && Object.values(fileAnns).some(a => a.outdated);
        const fileStatus = getGitStatus(entry.path);
        if (fileStatus) item.classList.add('git-' + fileStatus);

        item.innerHTML =
          `<svg><use href="${icons}#icon-file"/></svg>` +
          `<span>${escapeHtml(entry.name)}</span>` +
          (hasOutdated ? '<span class="comment-dot outdated-dot"></span>'
            : hasComments ? '<span class="comment-dot"></span>' : '');

        if (state.currentFile === entry.path) {
          item.classList.add('active');
          activeRow = item;
        }

        item.addEventListener('click', () => openFile(entry.path));
        container.appendChild(item);
      }
    });
  }

  // Load the open file's content and annotations into the code view. The view
  // ends up at scrollTop, which defaults to where it stands now so that a
  // reload after a change stays where you were.
  async function loadCurrentFile(annotations, scrollTop = codeContent.scrollTop) {
    const path = state.currentFile;
    if (!path) return;

    const [fileData, annData] = await Promise.all([
      api('GET', '/api/file?path=' + encodeURIComponent(path)),
      annotations || api('GET', '/api/annotations?path=' + encodeURIComponent(path)),
    ]);
    // Another file was opened while this one was loading
    if (state.currentFile !== path) return;

    state.language = fileData.language;
    state.diffLines = fileData.diffLines || {};
    state.diffHunks = fileData.diffHunks || [];
    state.diffDeletions = fileData.diffDeletions || [];
    state.annotations = annData;

    codeHeader.innerHTML = `
      <span class="file-path">${escapeHtml(path)}</span>
      <span class="lang-badge">${escapeHtml(state.language)}</span>
    `;
    codeContent.innerHTML = fileData.html;
    codeContent.scrollTop = scrollTop;
    renderCodeView();
    renderCommentList();
  }

  // Reload the open file after it changed on disk. Annotations that came with
  // the change are passed on rather than asked for a second time.
  async function refreshCurrentFile(annotations) {
    try {
      await loadCurrentFile(annotations);
    } catch (e) {
      console.error('Failed to refresh file:', e);
    }
  }

  // Open a file from the tree. Where the file being left stands is noted, and
  // the file being opened returns to where it was last left.
  async function openFile(path) {
    if (state.currentFile) {
      state.scrollPositions[state.currentFile] = codeContent.scrollTop;
    }
    state.currentFile = path;
    closeEditor();

    // Tell server to watch this file for changes
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'watch-file', path: path }));
    }

    codeHeader.innerHTML = `
      <span class="file-path">${escapeHtml(path)}</span>
      <span class="lang-badge">loading...</span>
    `;
    codeContent.innerHTML = '<div class="loading"><div class="spinner"></div> Loading...</div>';
    setActiveFile(path);

    try {
      await loadCurrentFile(null, state.scrollPositions[path] || 0);
    } catch (e) {
      codeContent.innerHTML = `<div class="empty-state"><p>Error loading file: ${escapeHtml(e.message)}</p></div>`;
    }
  }

  // The gutter cell of a line carries the id chroma generated for it, which
  // makes looking a line up a hash lookup rather than a walk over the file.
  function lineElement(lineNum) {
    const gutter = document.getElementById('L' + lineNum);
    return gutter ? gutter.parentElement : null;
  }

  // The line number an element sits on, or null for the deletion markers, which
  // stand between lines and have no number of their own.
  function lineNumberOf(lineEl) {
    const gutter = lineEl.firstElementChild;
    if (!gutter || !gutter.id) return null;
    const lineNum = parseInt(gutter.id.slice(1), 10);
    return Number.isInteger(lineNum) ? lineNum : null;
  }

  // The diff hunk a line belongs to, or null. Hunks arrive in order, so this
  // finds one without looking at every hunk.
  function hunkAt(lineNum) {
    let low = 0;
    let high = state.diffHunks.length - 1;
    while (low <= high) {
      const mid = (low + high) >> 1;
      const hunk = state.diffHunks[mid];
      if (lineNum < hunk.startLine) high = mid - 1;
      else if (lineNum > hunk.endLine) low = mid + 1;
      else return hunk;
    }
    return null;
  }

  // Handle clicks and gutter hovers for the whole code view. Registered once for
  // the container rather than per line, of which a large file holds tens of
  // thousands.
  function attachCodeViewHandlers() {
    codeContent.addEventListener('click', (e) => {
      const lineEl = e.target.closest('.line');
      if (!lineEl) return;
      // A drag that highlighted several lines is the range the user means; the
      // click that ends it must not shrink the comment back to one line.
      const hl = highlightedLineRange();
      if (hl && hl[1] > hl[0]) {
        state.anchorLine = hl[0];
        beginComment(hl[0], hl[1], true);
        return;
      }
      const lineNum = lineNumberOf(lineEl);
      if (lineNum !== null) clickLine(lineNum, e.shiftKey);
    });

    codeContent.addEventListener('mouseover', (e) => {
      const gutter = e.target.closest('.ln');
      if (!gutter) return;

      // A deletion marker names its hunk; a line is looked up by its number
      let hunk = null;
      if (gutter.dataset.hunkIndex !== undefined) {
        hunk = state.diffHunks[Number(gutter.dataset.hunkIndex)] || null;
      } else {
        const lineNum = lineNumberOf(gutter.parentElement);
        if (lineNum !== null && state.diffLines[lineNum]) hunk = hunkAt(lineNum);
      }
      if (hunk) showDiffTooltip(e, hunk);
    });
  }

  // Draw the diff and comment markers over the file just loaded. Only the lines
  // that carry one are touched.
  function renderCodeView() {
    // Counted before the deletion markers are inserted: those carry the same
    // class but stand between lines rather than being ones, and counting them
    // would push every scrollbar marker up the strip.
    state.totalLines = codeContent.querySelectorAll('.chroma .line').length;

    for (const [lineNum, type] of Object.entries(state.diffLines)) {
      const lineEl = lineElement(lineNum);
      if (lineEl) lineEl.classList.add('diff-' + type);
    }

    for (const [lineNum, ann] of Object.entries(state.annotations)) {
      const { start, end } = annotationRange(ann, Number(lineNum));
      for (let n = start; n <= end; n++) {
        const lineEl = lineElement(n);
        if (!lineEl) continue;
        lineEl.classList.add('has-comment');
        if (ann.outdated) lineEl.classList.add('has-outdated-comment');
      }
    }

    // Keep the lines being commented on marked after a redraw
    if (state.selectionStart && state.selectionEnd) {
      selectRange(state.selectionStart, state.selectionEnd);
    }

    renderDeletionMarkers();
    renderScrollbarMarkers();
  }

  // Take the comment markers off every line of the open file, for when the
  // annotations go away without the file being drawn again.
  function clearCommentMarkers() {
    codeContent.querySelectorAll('.has-comment').forEach(lineEl => {
      lineEl.classList.remove('has-comment', 'has-outdated-comment');
    });
  }

  // Insert a marker for each block of lines the diff removed.
  function renderDeletionMarkers() {
    state.diffDeletions.forEach(del => {
      const marker = document.createElement('span');
      marker.className = 'line diff-deleted-marker';

      const gutter = document.createElement('span');
      gutter.className = 'ln diff-del-gutter';
      gutter.textContent = '\u00a0'; // non-breaking space
      gutter.dataset.hunkIndex = del.hunkIndex;
      marker.appendChild(gutter);

      if (del.afterLine === 0) {
        // Deletion at top of file — insert before first line
        const firstLine = codeContent.querySelector('.chroma .line');
        if (firstLine) firstLine.before(marker);
      } else {
        const afterEl = lineElement(del.afterLine);
        if (afterEl) afterEl.after(marker);
      }
    });
  }

  // Render scrollbar markers for comments and diff lines
  function renderScrollbarMarkers() {
    let strip = document.querySelector('.scrollbar-markers');
    if (strip) strip.remove();

    const totalLines = state.totalLines;
    if (totalLines === 0) return;

    const markers = [];
    for (const [line, type] of Object.entries(state.diffLines)) {
      markers.push({ line: Number(line), color: type === 'added' ? '#16a34a' : '#d97706' });
    }
    for (const del of state.diffDeletions) {
      markers.push({ line: del.afterLine || 1, color: '#dc2626' });
    }
    for (const line of Object.keys(state.annotations)) {
      markers.push({ line: Number(line), color: 'rgb(37, 99, 235)' });
    }
    if (markers.length === 0) return;

    strip = document.createElement('div');
    strip.className = 'scrollbar-markers';

    positionScrollbarStrip(strip);

    markers.forEach(m => {
      const mark = document.createElement('div');
      mark.className = 'scrollbar-mark';
      mark.style.top = ((m.line - 1) / totalLines * 100) + '%';
      mark.style.backgroundColor = m.color;
      strip.appendChild(mark);
    });
    document.body.appendChild(strip);
  }

  function positionScrollbarStrip(strip) {
    const rect = codeContent.getBoundingClientRect();
    strip.style.top = rect.top + 'px';
    strip.style.right = (window.innerWidth - rect.right) + 'px';
    strip.style.height = rect.height + 'px';
  }


  // The lines a comment covers, in order. The annotation's key is its end line;
  // startLine is stored alongside it. A comment written before ranges existed
  // has no start line and covers the end line alone.
  function annotationRange(ann, endLine) {
    const end = ann && ann.endLine ? ann.endLine : endLine;
    let start = ann && ann.startLine ? ann.startLine : end;
    if (start < 1 || start > end) start = end;
    return { start, end };
  }

  // The lines currently marked. Remembered so they can be cleared without
  // walking the file.
  let selectedLineEls = [];

  // Mark a run of lines as the one being commented on.
  function selectRange(start, end) {
    selectedLineEls.forEach(el => el.classList.remove('selected'));
    selectedLineEls = [];
    for (let n = start; n <= end; n++) {
      const el = lineElement(n);
      if (el) {
        el.classList.add('selected');
        selectedLineEls.push(el);
      }
    }
  }

  // Arm the editor for a comment on a run of lines. focusText is set when the
  // comment follows a click, which is when stealing the caret is wanted; a
  // mouse highlight is left alone so the selection can still be dragged.
  function beginComment(start, end, focusText) {
    state.selectionStart = start;
    state.selectionEnd = end;
    state.editingLine = end; // the annotation is keyed by its end line
    selectRange(start, end);

    const ann = state.annotations[end];
    editorLineLabel.textContent = start === end ? 'Line ' + end : 'Lines ' + start + '\u2013' + end;
    editorTextarea.value = ann ? ann.comment : '';
    updateEditorVisibility();
    if (focusText) editorTextarea.focus();
  }

  // Click a line to add or edit a comment, extending it into a range when shift
  // is held. A plain click falls back on the annotation at the end line it lands
  // on, so the whole range it covers is what gets edited.
  function clickLine(lineNum, shiftKey) {
    let start, end;
    if (shiftKey && state.anchorLine) {
      start = Math.min(state.anchorLine, lineNum);
      end = Math.max(state.anchorLine, lineNum);
    } else {
      const existing = state.annotations[lineNum];
      if (existing) {
        ({ start, end } = annotationRange(existing, lineNum));
      } else {
        start = end = lineNum;
      }
      state.anchorLine = lineNum;
    }
    beginComment(start, end, true);
  }

  // The line a DOM node sits on, or null when it is not inside a line.
  function lineOfNode(node) {
    if (!node) return null;
    const el = node.nodeType === 1 ? node : node.parentElement;
    const lineEl = el && el.closest && el.closest('.line');
    return lineEl ? lineNumberOf(lineEl) : null;
  }

  // The line numbers a highlight in the code view covers, or null when there is
  // no such highlight. Both ends are read off the selected nodes, which is a
  // lookup each; only a selection whose ends fall outside the lines falls back
  // to measuring rectangles.
  function highlightedLineRange() {
    const sel = document.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
    if (!codeContent.contains(sel.anchorNode) || !codeContent.contains(sel.focusNode)) return null;

    const a = lineOfNode(sel.anchorNode);
    const b = lineOfNode(sel.focusNode);
    if (a !== null && b !== null) return [Math.min(a, b), Math.max(a, b)];

    const rects = Array.from(sel.getRangeAt(0).getClientRects());
    if (rects.length === 0) return null;
    const top = Math.min(...rects.map(r => r.top));
    const bottom = Math.max(...rects.map(r => r.bottom));
    let min = Infinity;
    let max = -Infinity;
    codeContent.querySelectorAll('.chroma .line').forEach(el => {
      const r = el.getBoundingClientRect();
      if (r.bottom <= top || r.top >= bottom) return;
      const n = lineNumberOf(el);
      if (n === null) return;
      if (n < min) min = n;
      if (n > max) max = n;
    });
    return min === Infinity ? null : [min, max];
  }

  // A highlight of one or more whole lines becomes the comment's range, so the
  // review covers exactly what was selected.
  let highlightSyncFrame = null;
  function syncCommentToHighlight() {
    if (!state.currentFile) return;
    const range = highlightedLineRange();
    if (!range) return;
    // Only react when the highlight actually moved, or every caret movement
    // inside the textarea would reopen the same range.
    if (range[0] === state.selectionStart && range[1] === state.selectionEnd) return;
    state.anchorLine = range[0];
    beginComment(range[0], range[1], false);
  }

  document.addEventListener('selectionchange', () => {
    if (highlightSyncFrame) return;
    highlightSyncFrame = requestAnimationFrame(() => {
      highlightSyncFrame = null;
      syncCommentToHighlight();
    });
  });

  // Save comment. A comment on more than one line is sent as a Gerrit-style
  // range: start_line and end_line, with the end line also as the plain line.
  async function saveComment() {
    if (!state.currentFile || !state.editingLine) return;
    const text = editorTextarea.value.trim();
    if (!text) return;

    const start = state.selectionStart || state.editingLine;
    const end = state.editingLine;
    const body = { path: state.currentFile, line: end, comment: text };
    if (start !== end) {
      body.range = { start_line: start, end_line: end };
    }

    try {
      await api('POST', '/api/annotations', body);

      const ann = { comment: text, startLine: start, endLine: end, outdated: false };
      state.annotations[end] = ann;
      if (!state.allAnnotations[state.currentFile]) {
        state.allAnnotations[state.currentFile] = {};
      }
      state.allAnnotations[state.currentFile][end] = ann;

      // Update gutter across the range
      for (let n = start; n <= end; n++) {
        const lineEl = lineElement(n);
        if (lineEl) {
          lineEl.classList.add('has-comment');
          lineEl.classList.remove('has-outdated-comment');
        }
      }

      annotationsChanged();
    } catch (e) {
      showToast('Failed to save: ' + e.message, true);
    }
  }

  // Delete comment
  async function deleteComment() {
    if (!state.currentFile || !state.editingLine) return;

    const end = state.editingLine;
    try {
      await api('DELETE', '/api/annotations', {
        path: state.currentFile,
        line: end,
      });

      const removed = state.annotations[end];
      const start = removed ? annotationRange(removed, end).start : end;
      delete state.annotations[end];
      if (state.allAnnotations[state.currentFile]) {
        delete state.allAnnotations[state.currentFile][end];
        if (Object.keys(state.allAnnotations[state.currentFile]).length === 0) {
          delete state.allAnnotations[state.currentFile];
        }
      }

      // Update gutter across the range
      for (let n = start; n <= end; n++) {
        const lineEl = lineElement(n);
        if (lineEl) {
          lineEl.classList.remove('has-comment');
          lineEl.classList.remove('has-outdated-comment');
        }
      }

      annotationsChanged();
    } catch (e) {
      showToast('Failed to delete: ' + e.message, true);
    }
  }

  // Put the editor away, leaving no line marked as being commented on.
  function closeEditor() {
    state.editingLine = null;
    state.selectionStart = null;
    state.selectionEnd = null;
    selectRange(0, -1);
    updateEditorVisibility();
  }

  // Bring everything that shows annotations back in line after one changed.
  function annotationsChanged() {
    closeEditor();
    renderCommentList();
    renderScrollbarMarkers();
    renderTree();
    updateCommentCount();
  }

  // Render comment list in sidebar
  function renderCommentList() {
    if (!commentList) return;

    const lines = Object.keys(state.annotations).map(Number).sort((a, b) => a - b);
    if (lines.length === 0) {
      commentList.innerHTML = '<div class="empty-state" style="padding:2rem"><small>No comments on this file.<br>Click a line to add one.</small></div>';
      return;
    }

    commentList.innerHTML = lines.map(lineNum => {
      const ann = state.annotations[lineNum];
      const { start, end } = annotationRange(ann, lineNum);
      const outdatedClass = ann && ann.outdated ? ' comment-outdated' : '';
      const outdatedBadge = ann && ann.outdated ? '<span class="outdated-badge">outdated</span>' : '';
      const label = start === end ? 'Line ' + end : 'Lines ' + start + '\u2013' + end;
      return `
        <div class="comment-item${outdatedClass}" data-comment-line="${lineNum}">
          <div class="comment-line">${label} ${outdatedBadge}</div>
          <div class="comment-text">${escapeHtml(ann ? ann.comment : '')}</div>
        </div>
      `;
    }).join('');

    // Attach click handlers
    commentList.querySelectorAll('.comment-item').forEach(item => {
      item.addEventListener('click', () => {
        const ln = parseInt(item.dataset.commentLine, 10);
        clickLine(ln);
        // Scroll to line
        const lineEl = lineElement(ln);
        if (lineEl) lineEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
      });
    });
  }

  // Update editor visibility
  function updateEditorVisibility() {
    if (commentEditor) {
      commentEditor.style.display = state.editingLine ? '' : 'none';
    }
  }

  // Update status bar comment count
  function updateCommentCount() {
    if (!statusCommentCount) return;
    let count = 0;
    for (const file in state.allAnnotations) {
      count += Object.keys(state.allAnnotations[file]).length;
    }
    statusCommentCount.textContent = count + ' comment' + (count !== 1 ? 's' : '');
  }

  // Escape HTML. Replacing the few characters that matter is a fraction of the
  // cost of routing the text through a throwaway element, and this runs for
  // every tree row, every comment and every line of a diff tooltip.
  const htmlEscapes = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
  function escapeHtml(str) {
    return String(str).replace(/[&<>"']/g, ch => htmlEscapes[ch]);
  }

  // Diff tooltip
  let diffTooltip = null;
  let diffTooltipHunk = null;
  let diffTooltipMoveHandler = null;
  let diffTooltipBounds = null;
  let diffTooltipRaf = null;

  function showDiffTooltip(event, hunk) {
    // Moving along the gutter of one hunk keeps the tooltip that is up, rather
    // than building the same one again for every cell passed over.
    if (diffTooltip && diffTooltipHunk === hunk) return;

    hideDiffTooltip();
    diffTooltipHunk = hunk;
    diffTooltip = document.createElement('div');
    diffTooltip.className = 'diff-tooltip';
    const lines = hunk.diff.trimEnd().split('\n');
    const html = lines.map(line => {
      const cls = line.startsWith('+') ? ' class="diff-add"' : line.startsWith('-') ? ' class="diff-del"' : '';
      return '<span' + cls + '>' + escapeHtml(line) + '</span>';
    }).join('\n');
    diffTooltip.innerHTML = '<pre>' + html + '</pre>';

    // Place the tooltip at the cursor straight away so the pointer is already
    // inside it (near the top-left corner). This lets a tall, scrollable
    // tooltip be reached and scrolled without the pointer having to leave it
    // first.
    const inset = 12;
    diffTooltip.style.left = Math.max(0, event.clientX - inset) + 'px';
    diffTooltip.style.top = Math.max(0, event.clientY - inset) + 'px';
    document.body.appendChild(diffTooltip);

    // Clamping the tooltip into the viewport needs its measured size, and
    // measuring forces the browser to lay out the page. On a large diff the
    // code view holds so many nodes that this layout costs hundreds of
    // milliseconds, and doing it here — while the pointer is crossing gutters
    // during a scroll — stalls the scroll until it finishes. So the measuring
    // and clamping are put off to the next frame, off the hover path, where the
    // layout has already settled and reading it back is cheap.
    const cursorX = event.clientX;
    const cursorY = event.clientY;
    diffTooltipRaf = requestAnimationFrame(() => {
      diffTooltipRaf = null;
      if (!diffTooltip) return;

      const margin = 16;
      const rect = diffTooltip.getBoundingClientRect();
      let left = cursorX - inset;
      let top = cursorY - inset;

      // Keep it inside the viewport by shifting (never flipping away from the
      // cursor, which would move it out from under the pointer).
      if (left + rect.width > window.innerWidth - margin) {
        left = window.innerWidth - margin - rect.width;
      }
      left = Math.max(margin, Math.min(left, cursorX));
      if (top + rect.height > window.innerHeight - margin) {
        top = window.innerHeight - margin - rect.height;
      }
      top = Math.max(margin, Math.min(top, cursorY));

      diffTooltip.style.left = left + 'px';
      diffTooltip.style.top = top + 'px';

      // The tooltip stays where it was put, so its bounds are measured once
      // here rather than on every movement of the pointer, which would force
      // the browser to work out the page's layout again each time.
      diffTooltipBounds = diffTooltip.getBoundingClientRect();
    });

    // Hide once the pointer leaves the tooltip. A document-level mousemove
    // guard is used instead of the gutter's mouseleave: the tooltip now sits
    // under the cursor (covering the gutter), so relying on the gutter would
    // hide it immediately and loop. Until the bounds are measured next frame
    // the pointer is still at the corner where the tooltip was placed, so there
    // is nothing to leave yet.
    diffTooltipMoveHandler = (e) => {
      const b = diffTooltipBounds;
      if (!b) return;
      if (e.clientX < b.left || e.clientX > b.right ||
          e.clientY < b.top || e.clientY > b.bottom) {
        hideDiffTooltip();
      }
    };
    document.addEventListener('mousemove', diffTooltipMoveHandler);
  }

  function hideDiffTooltip() {
    if (diffTooltipRaf) {
      cancelAnimationFrame(diffTooltipRaf);
      diffTooltipRaf = null;
    }
    if (diffTooltipMoveHandler) {
      document.removeEventListener('mousemove', diffTooltipMoveHandler);
      diffTooltipMoveHandler = null;
    }
    if (diffTooltip) {
      diffTooltip.remove();
      diffTooltip = null;
    }
    diffTooltipHunk = null;
    diffTooltipBounds = null;
  }

  // Start a new review (delete REVIEW.md)
  async function newReview() {
    if (!confirm('Start a new review? This will delete all existing comments.')) return;
    try {
      await api('DELETE', '/api/review');
      state.allAnnotations = {};
      state.annotations = {};
      clearCommentMarkers();
      annotationsChanged();
      showToast('New review started');
    } catch (e) {
      showToast('Failed to start new review: ' + e.message, true);
    }
  }

  // Expose functions for inline handlers
  window.app = { saveComment, deleteComment, closeEditor, newReview };

})();
