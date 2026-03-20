// Code Review Tool - Main Application
(function() {
  'use strict';

  const state = {
    tree: [],
    openDirs: {},
    currentFile: null,
    fileHtml: '',
    language: '',
    annotations: {},      // line → comment for current file
    allAnnotations: {},   // path → {line → comment} for all files
    editingLine: null,
    editingText: '',
    loading: false,
    gitStatuses: {},     // path → status string (modified, staged, untracked, added, deleted, conflict)
  };

  // DOM references
  let treeContainer, codeContent, codeHeader, commentList, commentEditor,
      editorTextarea, editorLineLabel, statusCommentCount, statusMdPath;

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

    loadTree();
    loadAllAnnotations();
    loadGitStatus();
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
        const hasComments = state.allAnnotations[entry.path] &&
                           Object.keys(state.allAnnotations[entry.path]).length > 0;
        const fileStatus = getGitStatus(entry.path);
        if (fileStatus) item.classList.add('git-' + fileStatus);

        item.innerHTML = `
          <svg><use href="/static/assets/icons.svg#icon-file"/></svg>
          <span>${escapeHtml(entry.name)}</span>
          ${hasComments ? '<span class="comment-dot"></span>' : ''}
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
      return state.allAnnotations[entry.path] &&
             Object.keys(state.allAnnotations[entry.path]).length > 0;
    }
    return entry.children && entry.children.some(c => hasAnnotationsInTree(c));
  }

  // Open a file
  async function openFile(path) {
    state.loading = true;
    state.currentFile = path;
    state.editingLine = null;
    updateEditorVisibility();

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

        // Mark lines with comments
        if (state.annotations[lineNum]) {
          lineEl.classList.add('has-comment');
        }

        lineEl.addEventListener('click', () => clickLine(lineNum));
      }
    });
  }

  // Click a line to add/edit comment
  function clickLine(lineNum) {
    // Deselect previous
    codeContent.querySelectorAll('.line.selected').forEach(el => el.classList.remove('selected'));

    // Select this line
    const lineEl = codeContent.querySelector(`.line[data-line-num="${lineNum}"]`);
    if (lineEl) lineEl.classList.add('selected');

    state.editingLine = lineNum;
    state.editingText = state.annotations[lineNum] || '';

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

      state.annotations[state.editingLine] = text;
      if (!state.allAnnotations[state.currentFile]) {
        state.allAnnotations[state.currentFile] = {};
      }
      state.allAnnotations[state.currentFile][state.editingLine] = text;

      // Update gutter
      const lineEl = codeContent.querySelector(`.line[data-line-num="${state.editingLine}"]`);
      if (lineEl) lineEl.classList.add('has-comment');

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
      if (lineEl) lineEl.classList.remove('has-comment');

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

    commentList.innerHTML = lines.map(lineNum => `
      <div class="comment-item" data-comment-line="${lineNum}">
        <div class="comment-line">Line ${lineNum}</div>
        <div class="comment-text">${escapeHtml(state.annotations[lineNum])}</div>
      </div>
    `).join('');

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

  // Expose functions for inline handlers
  window.app = { saveComment, deleteComment, cancelEdit };

})();
