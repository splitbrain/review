// Code Review Tool - Main Application
(function() {
  'use strict';

  const state = {
    tree: [],
    openDirs: {},
    currentFile: null,
    language: '',
    annotations: {},      // line → {comment, outdated} for current file
    allAnnotations: {},   // path → {line → {comment, outdated}} for all files
    diffLines: {},        // line → "added"|"modified" for current file
    diffHunks: [],        // [{startLine, endLine, diff}] for current file
    diffDeletions: [],    // [{afterLine, hunkIndex}] for current file
    totalLines: 0,        // number of lines in the current file
    scrollPositions: {},  // path → scrollTop of the code view when the file was left
    editingLine: null,
    gitStatuses: {},
    wsConnected: false,
    historyEnabled: false, // retired reviews are kept rather than deleted
    historyDest: '',       // where the current review would be moved on the next one
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
      applyConfig(config);
    } catch (e) {
      console.error('Failed to load config:', e);
    }
  }

  // Apply the review settings the config endpoint returned.
  function applyConfig(config) {
    state.historyEnabled = !!config.historyEnabled;
    state.historyDest = config.historyDest || '';
    if (config.base && statusBase) {
      statusBase.textContent = 'compared to ' + config.base;
      statusBase.title = 'Changes are highlighted relative to ' + config.base;
      statusBase.style.display = '';
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
      const lineNum = lineNumberOf(lineEl);
      if (lineNum !== null) clickLine(lineNum);
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
      const lineEl = lineElement(lineNum);
      if (!lineEl) continue;
      lineEl.classList.add('has-comment');
      if (ann.outdated) lineEl.classList.add('has-outdated-comment');
    }

    // Keep the line being commented on marked after a redraw
    if (state.editingLine) selectLine(lineElement(state.editingLine));

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


  // Mark a line as the one being commented on. The previous one is remembered
  // rather than searched for again.
  let selectedLineEl = null;
  function selectLine(lineEl) {
    if (selectedLineEl) selectedLineEl.classList.remove('selected');
    selectedLineEl = lineEl;
    if (lineEl) lineEl.classList.add('selected');
  }

  // Click a line to add/edit comment
  function clickLine(lineNum) {
    selectLine(lineElement(lineNum));

    state.editingLine = lineNum;
    const ann = state.annotations[lineNum];

    editorLineLabel.textContent = 'Line ' + lineNum;
    editorTextarea.value = ann ? ann.comment : '';
    updateEditorVisibility();
    editorTextarea.focus();
  }

  // Save comment
  async function saveComment() {
    if (!state.currentFile || !state.editingLine) return;
    const text = editorTextarea.value.trim();
    if (!text) return;

    try {
      await api('POST', '/api/annotations', {
        path: state.currentFile,
        line: state.editingLine,
        comment: text,
      });

      state.annotations[state.editingLine] = { comment: text, outdated: false };
      if (!state.allAnnotations[state.currentFile]) {
        state.allAnnotations[state.currentFile] = {};
      }
      state.allAnnotations[state.currentFile][state.editingLine] = { comment: text, outdated: false };

      // Update gutter
      const lineEl = lineElement(state.editingLine);
      if (lineEl) {
        lineEl.classList.add('has-comment');
        lineEl.classList.remove('has-outdated-comment');
      }

      annotationsChanged();
    } catch (e) {
      showToast('Failed to save: ' + e.message, true);
    }
  }

  // Delete comment
  async function deleteComment() {
    if (!state.currentFile || !state.editingLine) return;

    try {
      await api('DELETE', '/api/annotations', {
        path: state.currentFile,
        line: state.editingLine,
      });

      delete state.annotations[state.editingLine];
      if (state.allAnnotations[state.currentFile]) {
        delete state.allAnnotations[state.currentFile][state.editingLine];
        if (Object.keys(state.allAnnotations[state.currentFile]).length === 0) {
          delete state.allAnnotations[state.currentFile];
        }
      }

      // Update gutter
      const lineEl = lineElement(state.editingLine);
      if (lineEl) {
        lineEl.classList.remove('has-comment');
        lineEl.classList.remove('has-outdated-comment');
      }

      annotationsChanged();
    } catch (e) {
      showToast('Failed to delete: ' + e.message, true);
    }
  }

  // Put the editor away, leaving no line marked as being commented on.
  function closeEditor() {
    state.editingLine = null;
    selectLine(null);
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
      const outdatedClass = ann && ann.outdated ? ' comment-outdated' : '';
      const outdatedBadge = ann && ann.outdated ? '<span class="outdated-badge">outdated</span>' : '';
      return `
        <div class="comment-item${outdatedClass}" data-comment-line="${lineNum}">
          <div class="comment-line">Line ${lineNum} ${outdatedBadge}</div>
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

  // Start a new review, moving the current one into the review history when
  // it is set up and deleting it otherwise.
  async function newReview() {
    // History may have been set up outside this page, and the destination is
    // dated on retirement, so the settings are read fresh for the prompt.
    try {
      applyConfig(await api('GET', '/api/config'));
    } catch (e) {
      console.error('Failed to reload config:', e);
    }

    let question;
    if (state.historyEnabled && state.historyDest) {
      question = 'Start a new review? The current REVIEW.md will be moved to ' +
        state.historyDest + '. Confirm?';
    } else if (state.historyEnabled) {
      question = 'Start a new review? Confirm?';
    } else {
      question = 'Start a new review? This will delete all existing comments.';
    }
    if (!confirm(question)) return;

    try {
      const result = await api('DELETE', '/api/review');
      state.allAnnotations = {};
      state.annotations = {};
      clearCommentMarkers();
      annotationsChanged();
      if (result.archived) {
        showToast('New review started — the old one was moved to ' + result.archived);
      } else {
        showToast('New review started');
      }
      // Refresh the destination named for the next review.
      loadConfig();
    } catch (e) {
      showToast('Failed to start new review: ' + e.message, true);
    }
  }

  // Expose functions for inline handlers
  window.app = { saveComment, deleteComment, closeEditor, newReview };

})();
