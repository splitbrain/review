// Code Review Tool - Main Application
(function() {
  'use strict';

  const state = {
    tree: [],
    openDirs: {},
    currentFile: null,
    fileHtml: '',
    language: '',
    annotations: {},      // line → {comment, outdated} for current file
    allAnnotations: {},   // path → {line → {comment, outdated}} for all files
    diffLines: {},        // line → "added"|"modified" for current file
    diffHunks: [],        // [{startLine, endLine, diff}] for current file
    diffDeletions: [],    // [{afterLine, count, hunkIndex}] for current file
    editingLine: null,
    editingText: '',
    loading: false,
    gitStatuses: {},
    wsConnected: false,
  };

  let ws = null;
  let wsReconnectDelay = 1000;

  // DOM references
  let treeContainer, codeContent, codeHeader, commentList, commentEditor,
      editorTextarea, editorLineLabel, statusCommentCount, statusMdPath,
      wsIndicator, toastContainer;

  // Initialize
  document.addEventListener('DOMContentLoaded', () => {
    treeContainer = document.getElementById('tree-container');
    codeContent = document.getElementById('code-content');
    codeHeader = document.getElementById('code-header');
    commentList = document.getElementById('comment-list');
    commentEditor = document.getElementById('comment-editor');
    editorTextarea = document.getElementById('editor-textarea');
    editorLineLabel = document.getElementById('editor-line-label');
    statusCommentCount = document.getElementById('status-comment-count');
    statusMdPath = document.getElementById('status-md-path');
    wsIndicator = document.getElementById('ws-indicator');
    toastContainer = document.getElementById('toast-container');

    loadTree();
    loadAllAnnotations();
    loadGitStatus();
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
        showToast('File changed: ' + msg.path);
        // Update allAnnotations for this file
        if (msg.annotations) {
          state.allAnnotations[msg.path] = msg.annotations;
        }
        // If we're viewing this file, refresh it
        if (state.currentFile === msg.path) {
          refreshCurrentFile();
        }
        updateCommentCount();
        renderTree();
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
        // Also refresh git status and tree (file may have new diff status)
        loadGitStatus();
        break;

      case 'review-reloaded':
        showToast('REVIEW.md reloaded from disk');
        if (msg.allAnnotations) {
          state.allAnnotations = msg.allAnnotations;
        } else {
          loadAllAnnotations();
        }
        if (state.currentFile) {
          refreshCurrentFile();
        }
        updateCommentCount();
        renderTree();
        break;

      case 'server-shutdown':
        // Server is shutting down — close the tab
        document.title = 'Review — closed';
        window.close();
        // Fallback if window.close() is blocked by browser policy
        document.body.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;height:100vh;color:var(--pico-muted)"><p>Server stopped. You can close this tab.</p></div>';
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

  // Load file tree
  async function loadTree() {
    try {
      state.tree = await api('GET', '/api/tree');
      renderTree();
    } catch (e) {
      console.error('Failed to load tree:', e);
    }
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

  // Load git status
  async function loadGitStatus() {
    try {
      state.gitStatuses = await api('GET', '/api/git-status');
      renderTree();
    } catch (e) {
      console.error('Failed to load git status:', e);
    }
  }

  // Get git status for a file path
  function getGitStatus(path) {
    return state.gitStatuses[path] || '';
  }

  // Get aggregate git status for a directory (most important child status)
  function getDirGitStatus(entry) {
    if (!entry.isDir) return getGitStatus(entry.path);
    if (!entry.children) return '';
    const priorities = { conflict: 5, modified: 4, untracked: 3, added: 2, staged: 1, deleted: 4 };
    let best = '';
    let bestPri = 0;
    for (const child of entry.children) {
      const s = getDirGitStatus(child);
      if (s && (priorities[s] || 0) > bestPri) {
        best = s;
        bestPri = priorities[s] || 0;
      }
    }
    return best;
  }

  // Render file tree
  function renderTree() {
    treeContainer.innerHTML = '';
    renderTreeLevel(state.tree, treeContainer, 0);
  }

  function renderTreeLevel(entries, container, depth) {
    entries.forEach(entry => {
      const item = document.createElement('div');
      item.className = 'tree-item';
      item.style.paddingLeft = (0.5 + depth * 1) + 'rem';

      if (entry.isDir) {
        const isOpen = state.openDirs[entry.path] || false;
        const dirStatus = getDirGitStatus(entry);
        if (dirStatus) item.classList.add('git-' + dirStatus);

        item.innerHTML = `
          <svg class="chevron ${isOpen ? 'open' : ''}"><use href="/static/assets/icons.svg#icon-chevron-right"/></svg>
          <svg><use href="/static/assets/icons.svg#icon-folder${isOpen ? '-open' : ''}"/></svg>
          <span>${escapeHtml(entry.name)}</span>
        `;

        const hasComments = entry.children && hasAnnotationsInTree(entry);
        if (hasComments) {
          item.innerHTML += '<span class="comment-dot"></span>';
        }

        item.addEventListener('click', () => {
          state.openDirs[entry.path] = !state.openDirs[entry.path];
          renderTree();
        });

        container.appendChild(item);

        if (entry.children) {
          const childContainer = document.createElement('div');
          childContainer.className = 'tree-children' + (isOpen ? ' open' : '');
          renderTreeLevel(entry.children, childContainer, depth + 1);
          container.appendChild(childContainer);
        }
      } else {
        const fileAnns = state.allAnnotations[entry.path];
        const hasComments = fileAnns && Object.keys(fileAnns).length > 0;
        const hasOutdated = hasComments && Object.values(fileAnns).some(a => a.outdated);
        const fileStatus = getGitStatus(entry.path);
        if (fileStatus) item.classList.add('git-' + fileStatus);

        item.innerHTML = `
          <svg><use href="/static/assets/icons.svg#icon-file"/></svg>
          <span>${escapeHtml(entry.name)}</span>
          ${hasOutdated ? '<span class="comment-dot outdated-dot"></span>' : hasComments ? '<span class="comment-dot"></span>' : ''}
        `;

        if (state.currentFile === entry.path) {
          item.classList.add('active');
        }

        item.addEventListener('click', () => openFile(entry.path));
        container.appendChild(item);
      }
    });
  }

  function hasAnnotationsInTree(entry) {
    if (!entry.isDir) {
      const anns = state.allAnnotations[entry.path];
      return anns && Object.keys(anns).length > 0;
    }
    return entry.children && entry.children.some(c => hasAnnotationsInTree(c));
  }

  // Refresh current file (re-fetch annotations, keep scroll position)
  async function refreshCurrentFile() {
    if (!state.currentFile) return;
    try {
      const [fileData, annData] = await Promise.all([
        api('GET', '/api/file?path=' + encodeURIComponent(state.currentFile)),
        api('GET', '/api/annotations?path=' + encodeURIComponent(state.currentFile)),
      ]);
      state.fileHtml = fileData.html;
      state.language = fileData.language;
      state.diffLines = fileData.diffLines || {};
      state.diffHunks = fileData.diffHunks || [];
      state.diffDeletions = fileData.diffDeletions || [];
      state.annotations = annData;
      codeContent.innerHTML = state.fileHtml;
      attachLineHandlers();
      renderCommentList();
    } catch (e) {
      console.error('Failed to refresh file:', e);
    }
  }

  // Open a file
  async function openFile(path) {
    state.loading = true;
    state.currentFile = path;
    state.editingLine = null;
    updateEditorVisibility();

    // Tell server to watch this file for changes
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'watch-file', path: path }));
    }

    codeHeader.innerHTML = `
      <span class="file-path">${escapeHtml(path)}</span>
      <span class="lang-badge">loading...</span>
    `;
    codeContent.innerHTML = '<div class="loading"><div class="spinner"></div> Loading...</div>';

    try {
      const [fileData, annData] = await Promise.all([
        api('GET', '/api/file?path=' + encodeURIComponent(path)),
        api('GET', '/api/annotations?path=' + encodeURIComponent(path)),
      ]);

      state.fileHtml = fileData.html;
      state.language = fileData.language;
      state.diffLines = fileData.diffLines || {};
      state.diffHunks = fileData.diffHunks || [];
      state.diffDeletions = fileData.diffDeletions || [];
      state.annotations = annData;

      codeHeader.innerHTML = `
        <span class="file-path">${escapeHtml(path)}</span>
        <span class="lang-badge">${escapeHtml(state.language)}</span>
      `;

      codeContent.innerHTML = state.fileHtml;
      attachLineHandlers();
      renderCommentList();
      renderTree(); // update active state
    } catch (e) {
      codeContent.innerHTML = `<div class="empty-state"><p>Error loading file: ${escapeHtml(e.message)}</p></div>`;
    }

    state.loading = false;
  }

  // Attach click handlers to code lines
  function attachLineHandlers() {
    const lines = codeContent.querySelectorAll('.chroma .line');
    lines.forEach(lineEl => {
      // Find line number from the anchor or lnt span
      const anchor = lineEl.querySelector('a[id^="L"]') || lineEl.querySelector('[id^="L"]');
      let lineNum = null;

      if (anchor) {
        const id = anchor.id || anchor.getAttribute('id');
        if (id) {
          lineNum = parseInt(id.replace('L', ''), 10);
        }
      }

      if (!lineNum) {
        // Try to find from lnt span
        const lnt = lineEl.querySelector('.lnt, .ln');
        if (lnt) {
          lineNum = parseInt(lnt.textContent.trim(), 10);
        }
      }

      if (lineNum && !isNaN(lineNum)) {
        lineEl.dataset.lineNum = lineNum;

        // Mark changed lines (git diff)
        const diffType = state.diffLines[lineNum];
        if (diffType) {
          lineEl.classList.add('diff-' + diffType);

          // Show diff tooltip on gutter (line number) hover
          const hunk = state.diffHunks.find(h => lineNum >= h.startLine && lineNum <= h.endLine);
          if (hunk) {
            const gutter = lineEl.querySelector('.lnt, .ln');
            if (gutter) {
              gutter.addEventListener('mouseenter', (e) => showDiffTooltip(e, hunk));
              gutter.addEventListener('mouseleave', hideDiffTooltip);
            }
          }
        }

        // Mark lines with comments
        const ann = state.annotations[lineNum];
        if (ann) {
          lineEl.classList.add('has-comment');
          if (ann.outdated) {
            lineEl.classList.add('has-outdated-comment');
          }
        }

        lineEl.addEventListener('click', () => clickLine(lineNum));
      }
    });

    // Inject deletion markers
    state.diffDeletions.forEach(del => {
      const marker = document.createElement('span');
      marker.className = 'line diff-deleted-marker';

      const gutterSpan = document.createElement('span');
      gutterSpan.className = 'ln diff-del-gutter';
      gutterSpan.textContent = '\u00a0'; // non-breaking space
      marker.appendChild(gutterSpan);

      // Wire up tooltip on gutter hover
      const hunk = del.hunkIndex >= 0 ? state.diffHunks[del.hunkIndex] : null;
      if (hunk) {
        gutterSpan.addEventListener('mouseenter', (e) => showDiffTooltip(e, hunk));
        gutterSpan.addEventListener('mouseleave', hideDiffTooltip);
      }

      // Insert after the appropriate line
      if (del.afterLine === 0) {
        // Deletion at top of file — insert before first line
        const firstLine = codeContent.querySelector('.chroma .line');
        if (firstLine) firstLine.parentNode.insertBefore(marker, firstLine);
      } else {
        const afterEl = codeContent.querySelector(`.line[data-line-num="${del.afterLine}"]`);
        if (afterEl) afterEl.insertAdjacentElement('afterend', marker);
      }
    });

    renderScrollbarMarkers();
  }

  // Render scrollbar markers for comments and diff lines
  function renderScrollbarMarkers() {
    let strip = document.querySelector('.scrollbar-markers');
    if (strip) strip.remove();

    const totalLines = codeContent.querySelectorAll('.chroma .line').length;
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


  // Click a line to add/edit comment
  function clickLine(lineNum) {
    // Deselect previous
    codeContent.querySelectorAll('.line.selected').forEach(el => el.classList.remove('selected'));

    // Select this line
    const lineEl = codeContent.querySelector(`.line[data-line-num="${lineNum}"]`);
    if (lineEl) lineEl.classList.add('selected');

    state.editingLine = lineNum;
    const ann = state.annotations[lineNum];
    state.editingText = ann ? ann.comment : '';

    editorLineLabel.textContent = 'Line ' + lineNum;
    editorTextarea.value = state.editingText;
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
      const lineEl = codeContent.querySelector(`.line[data-line-num="${state.editingLine}"]`);
      if (lineEl) {
        lineEl.classList.add('has-comment');
        lineEl.classList.remove('has-outdated-comment');
      }

      state.editingLine = null;
      updateEditorVisibility();
      renderCommentList();
      renderTree();
      updateCommentCount();
    } catch (e) {
      alert('Failed to save: ' + e.message);
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
      const lineEl = codeContent.querySelector(`.line[data-line-num="${state.editingLine}"]`);
      if (lineEl) {
        lineEl.classList.remove('has-comment');
        lineEl.classList.remove('has-outdated-comment');
      }

      state.editingLine = null;
      updateEditorVisibility();
      renderCommentList();
      renderTree();
      updateCommentCount();
    } catch (e) {
      alert('Failed to delete: ' + e.message);
    }
  }

  // Cancel editing
  function cancelEdit() {
    state.editingLine = null;
    codeContent.querySelectorAll('.line.selected').forEach(el => el.classList.remove('selected'));
    updateEditorVisibility();
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
        const lineEl = codeContent.querySelector(`.line[data-line-num="${ln}"]`);
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

  // Escape HTML
  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // Diff tooltip
  let diffTooltip = null;

  function showDiffTooltip(event, hunk) {
    hideDiffTooltip();
    diffTooltip = document.createElement('div');
    diffTooltip.className = 'diff-tooltip';
    const lines = hunk.diff.trimEnd().split('\n');
    const html = lines.map(line => {
      const cls = line.startsWith('+') ? ' class="diff-add"' : line.startsWith('-') ? ' class="diff-del"' : '';
      return '<span' + cls + '>' + escapeHtml(line) + '</span>';
    }).join('\n');
    diffTooltip.innerHTML = '<pre>' + html + '</pre>';

    document.body.appendChild(diffTooltip);
    const rect = event.target.getBoundingClientRect();
    diffTooltip.style.top = (rect.bottom + 4) + 'px';
    diffTooltip.style.left = rect.left + 'px';

    // Ensure it doesn't overflow viewport
    const tooltipRect = diffTooltip.getBoundingClientRect();
    if (tooltipRect.right > window.innerWidth - 16) {
      diffTooltip.style.left = (window.innerWidth - tooltipRect.width - 16) + 'px';
    }
    if (tooltipRect.bottom > window.innerHeight - 16) {
      diffTooltip.style.top = (rect.top - tooltipRect.height - 4) + 'px';
    }
  }

  function hideDiffTooltip() {
    if (diffTooltip) {
      diffTooltip.remove();
      diffTooltip = null;
    }
  }

  // Start a new review (delete REVIEW.md)
  async function newReview() {
    if (!confirm('Start a new review? This will delete all existing comments.')) return;
    try {
      await api('DELETE', '/api/review');
      state.allAnnotations = {};
      state.annotations = {};
      state.editingLine = null;
      updateEditorVisibility();
      updateCommentCount();
      renderCommentList();
      renderTree();
      showToast('New review started');
    } catch (e) {
      alert('Failed to start new review: ' + e.message);
    }
  }

  // Expose functions for inline handlers
  window.app = { saveComment, deleteComment, cancelEdit, newReview };

})();
