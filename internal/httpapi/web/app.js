const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];

const state = {
  token: '',
  page: 0,
  total: 0,
  selectedSlug: null,
  selectedStatus: 'draft',
  requests: [],
};

class APIError extends Error {
  constructor(result) {
    super(result.data?.error || `Request failed (${result.status})`);
    this.result = result;
  }
}

function toast(message, isError = false) {
  const node = $('#toast');
  node.textContent = message;
  node.classList.toggle('error', isError);
  node.classList.add('show');
  clearTimeout(toast.timeout);
  toast.timeout = setTimeout(() => node.classList.remove('show'), 3500);
}

function safePath(path) {
  if (!path.startsWith('/') || path.startsWith('//')) throw new Error('Use a path on this API, such as /posts.');
  const url = new URL(path, location.origin);
  if (url.origin !== location.origin) throw new Error('Requests must stay on this API origin.');
  return url.pathname + url.search;
}

async function api(method, path, { body, auth = false, track = true } = {}) {
  path = safePath(path);
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (auth) {
    if (!state.token) throw new Error('Connect an admin token in the Editor tab first.');
    headers.Authorization = `Bearer ${state.token}`;
  }
  const started = performance.now();
  let result;
  try {
    const response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store',
    });
    const text = await response.text();
    let data = null;
    if (text) {
      try { data = JSON.parse(text); } catch { data = text; }
    }
    result = {
      method, path, status: response.status, data,
      duration: Math.round(performance.now() - started),
      cache: response.headers.get('X-Cache') || 'MISS',
      ok: response.ok,
    };
    if (track) addRequest(result);
    if (!response.ok) throw new APIError(result);
    return result;
  } catch (error) {
    if (!(error instanceof APIError) && track) {
      addRequest({ method, path, status: 'ERR', data: { error: error.message }, duration: Math.round(performance.now() - started), cache: '—', ok: false });
    }
    throw error;
  }
}

function addRequest(result) {
  state.requests.unshift(result);
  state.requests = state.requests.slice(0, 25);
  $('#requestCount').textContent = `${state.requests.length} calls`;
  const log = $('#requestLog');
  log.replaceChildren();
  state.requests.forEach((item) => {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'log-row';
    const method = document.createElement('strong');
    method.textContent = item.method;
    const path = document.createElement('span');
    path.className = 'path';
    path.textContent = item.path;
    const status = document.createElement('span');
    status.textContent = item.status;
    if (!item.ok) status.className = 'error';
    const duration = document.createElement('span');
    duration.textContent = `${item.duration}ms`;
    row.append(method, path, status, duration);
    row.addEventListener('click', () => showResponse(item));
    log.append(row);
  });
}

function showResponse(result) {
  const status = $('#responseStatus');
  status.textContent = result.status;
  status.className = `response-status ${result.ok ? 'success' : 'failure'}`;
  $('#responseTime').textContent = `${result.duration} ms`;
  $('#responseCache').textContent = `Cache: ${result.cache}`;
  $('#responseBody').textContent = result.data === null ? '(empty response)' : JSON.stringify(result.data, null, 2);
}

function navigate(section) {
  if (location.pathname.startsWith('/stories/')) {
    location.assign(`/${section === 'explore' ? '' : `#${section}`}`);
    return;
  }
  ['explore', 'editor', 'console'].forEach((name) => {
    $(`#${name}Panel`).hidden = name !== section;
    $$(`[data-nav="${name}"]`).forEach((item) => item.classList.toggle('active', name === section));
  });
  history.replaceState(null, '', section === 'explore' ? '/' : `/#${section}`);
  if (section === 'editor' && state.token) loadAdminPosts();
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

async function health() {
  try {
    await api('GET', '/ready', { track: false });
    $('#healthDot').className = 'status-dot online';
    $('#healthText').textContent = 'Site connected';
  } catch {
    $('#healthDot').className = 'status-dot offline';
    $('#healthText').textContent = 'Site unavailable';
  }
}

function setSelectOptions(select, items, label) {
  const current = select.value;
  select.replaceChildren(new Option(label, ''));
  items.forEach((item) => select.add(new Option(`${item.name} (${item.count})`, item.name)));
  if ([...select.options].some((option) => option.value === current)) select.value = current;
}

async function loadSummary() {
  try {
    const [posts, categories, tags] = await Promise.all([
      api('GET', '/posts?limit=1', { track: false }),
      api('GET', '/categories', { track: false }),
      api('GET', '/tags', { track: false }),
    ]);
    $('#articleCount').textContent = posts.data.total;
    setSelectOptions($('#categoryFilter'), categories.data.categories, 'All categories');
    setSelectOptions($('#tagFilter'), tags.data.tags, 'All tags');
    const topicList = $('#topicList');
    topicList.replaceChildren();
    if (!categories.data.categories.length) {
      const empty = document.createElement('span');
      empty.textContent = 'Topics will appear as articles are published.';
      topicList.append(empty);
    }
    categories.data.categories.forEach((item) => {
      const button = document.createElement('button');
      button.type = 'button';
      const name = document.createElement('span');
      name.textContent = item.name;
      const count = document.createElement('span');
      count.textContent = item.count;
      button.append(name, count);
      button.addEventListener('click', () => {
        $('#categoryFilter').value = item.name;
        state.page = 0;
        loadPosts();
        $('#latest').scrollIntoView({ behavior: 'smooth' });
      });
      topicList.append(button);
    });
  } catch (error) {
    toast(error.message, true);
  }
}

async function loadPosts() {
  const params = new URLSearchParams({ limit: '9', offset: String(state.page * 9) });
  const q = $('#searchInput').value.trim();
  if (q) params.set('q', q);
  if ($('#categoryFilter').value) params.set('category', $('#categoryFilter').value);
  if ($('#tagFilter').value) params.set('tag', $('#tagFilter').value);
  if ($('#featuredFilter').checked) params.set('featured', 'true');
  const grid = $('#postGrid');
  grid.replaceChildren();
  $('#leadPost').replaceChildren();
  const loading = document.createElement('div');
  loading.className = 'empty-state';
  loading.textContent = 'Loading articles…';
  grid.append(loading);
  try {
    const result = await api('GET', `/posts?${params}`);
    state.total = result.data.total;
    renderPosts(result.data.posts);
    const start = state.total ? state.page * 9 + 1 : 0;
    const end = Math.min(state.total, state.page * 9 + result.data.posts.length);
    $('#pageInfo').textContent = `Showing ${start}–${end} of ${state.total}`;
    $('#previousPage').disabled = state.page === 0;
    $('#nextPage').disabled = (state.page + 1) * 9 >= state.total;
  } catch (error) {
    grid.replaceChildren();
    const message = document.createElement('div');
    message.className = 'empty-state';
    message.textContent = error.message;
    grid.append(message);
    toast(error.message, true);
  }
}

function formatDate(value) {
  if (!value) return 'Recently';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Recently' : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function renderPosts(posts) {
  const grid = $('#postGrid');
  const lead = $('#leadPost');
  grid.replaceChildren();
  lead.replaceChildren();
  if (!posts.length) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    const title = document.createElement('strong');
    title.textContent = state.total ? 'No more articles here.' : 'Nothing published here yet.';
    const copy = document.createElement('p');
    copy.textContent = 'Try another filter or create your first post in the Editor.';
    empty.append(title, copy);
    grid.append(empty);
    return;
  }
  if (state.page === 0) {
    const post = posts[0];
    if (post.cover_image) lead.classList.add('has-image');
    else lead.classList.remove('has-image');
    const copy = document.createElement('div');
    copy.className = 'lead-copy';
    const label = document.createElement('span');
    label.className = 'lead-label';
    label.textContent = post.featured ? 'Featured story' : 'Latest story';
    const title = document.createElement('h2');
    const titleLink = document.createElement('a');
    titleLink.href = articleURL(post.slug);
    titleLink.textContent = post.title;
    title.append(titleLink);
    const summary = document.createElement('p');
    summary.textContent = post.summary;
    const meta = storyMeta(post);
    const read = document.createElement('a');
    read.href = articleURL(post.slug);
    read.className = 'read-link';
    read.textContent = 'Read the story →';
    copy.append(label, title, summary, meta, read);
    lead.append(copy);
    if (post.cover_image) {
      const media = document.createElement('div');
      media.className = 'lead-media';
      const image = document.createElement('img');
      image.src = post.cover_image;
      image.alt = '';
      image.addEventListener('error', () => { media.remove(); lead.classList.remove('has-image'); });
      media.append(image);
      lead.append(media);
    }
  }
  const remaining = state.page === 0 ? posts.slice(1) : posts;
  if (!remaining.length) {
    const note = document.createElement('p');
    note.className = 'empty-state';
    note.textContent = 'More articles will appear here soon.';
    grid.append(note);
  }
  remaining.forEach((post) => {
    const row = document.createElement('a');
    row.href = articleURL(post.slug);
    row.className = `post-row ${post.cover_image ? '' : 'no-image'}`;
    row.setAttribute('aria-label', `Read ${post.title}`);
    const copy = document.createElement('div');
    const meta = storyMeta(post);
    const title = document.createElement('h3');
    title.textContent = post.title;
    const summary = document.createElement('p');
    summary.textContent = post.summary;
    const more = document.createElement('span');
    more.className = 'read-link';
    more.textContent = 'Read article →';
    copy.append(meta, title, summary, more);
    row.append(copy);
    if (post.cover_image) {
      const image = document.createElement('img');
      image.className = 'post-row-media';
      image.src = post.cover_image;
      image.alt = '';
      image.loading = 'lazy';
      image.addEventListener('error', () => { image.remove(); row.classList.add('no-image'); });
      row.append(image);
    }
    grid.append(row);
  });
}

function articleURL(slug) {
  return `/stories/${encodeURIComponent(slug)}`;
}

function storyMeta(post) {
  const meta = document.createElement('div');
  meta.className = 'story-meta';
  const category = document.createElement('span');
  category.className = 'category';
  category.textContent = post.category;
  const divider = document.createElement('span');
  divider.textContent = '·';
  const date = document.createElement('span');
  date.textContent = formatDate(post.published_at || post.created_at);
  meta.append(category, divider, date);
  return meta;
}

async function loadArticlePage(slug) {
  $('#explorePanel').hidden = true;
  $('#editorPanel').hidden = true;
  $('#consolePanel').hidden = true;
  $('#articlePanel').hidden = false;
  $$('[data-nav]').forEach((item) => item.classList.remove('active'));
  const root = $('#articleContent');
  root.textContent = 'Loading article…';
  try {
    const { data: post } = await api('GET', `/posts/${encodeURIComponent(slug)}`);
    root.replaceChildren();
    document.title = `${post.title} — The Journal`;
    const header = document.createElement('header');
    header.className = 'article-header';
    const category = document.createElement('span');
    category.className = 'overline';
    category.textContent = post.category;
    const title = document.createElement('h1');
    title.textContent = post.title;
    const summary = document.createElement('p');
    summary.className = 'summary';
    summary.textContent = post.summary;
    const meta = document.createElement('div');
    meta.className = 'article-meta';
    meta.textContent = `${post.author || 'Editorial Team'} · ${formatDate(post.published_at || post.created_at)}`;
    header.append(category, title, summary, meta);
    root.append(header);
    if (post.cover_image) {
      const image = document.createElement('img');
      image.className = 'article-cover';
      image.src = post.cover_image;
      image.alt = '';
      image.addEventListener('error', () => image.remove());
      root.append(image);
    }
    const body = document.createElement('div');
    body.className = 'article-body';
    post.body.split(/\n{2,}/).forEach((paragraph) => {
      const p = document.createElement('p');
      p.textContent = paragraph;
      body.append(p);
    });
    root.append(body);
    if (post.tags?.length) {
      const tags = document.createElement('div');
      tags.className = 'article-tags';
      post.tags.forEach((tag) => { const chip = document.createElement('span'); chip.textContent = `#${tag}`; tags.append(chip); });
      root.append(tags);
    }
    await loadRelated(slug);
  } catch (error) {
    document.title = 'Article unavailable — The Journal';
    root.replaceChildren();
    const heading = document.createElement('h1');
    heading.textContent = 'This article is unavailable.';
    const copy = document.createElement('p');
    copy.textContent = error.message;
    root.append(heading, copy);
    $('#relatedSection').hidden = true;
  }
}

async function loadRelated(slug) {
  const section = $('#relatedSection');
  const list = $('#relatedPosts');
  section.hidden = true;
  list.replaceChildren();
  try {
    const { data } = await api('GET', `/posts/${encodeURIComponent(slug)}/related?limit=3`, { track: false });
    if (!data.posts.length) return;
    data.posts.forEach((post) => {
      const link = document.createElement('a');
      link.href = articleURL(post.slug);
      link.className = 'related-card';
      const label = document.createElement('span');
      label.className = 'overline';
      label.textContent = post.category;
      const title = document.createElement('h3');
      title.textContent = post.title;
      const more = document.createElement('span');
      more.textContent = 'Read article →';
      link.append(label, title, more);
      list.append(link);
    });
    section.hidden = false;
  } catch {
    section.hidden = true;
  }
}

function resetEditor() {
  state.selectedSlug = null;
  state.selectedStatus = 'draft';
  $('#editorForm').reset();
  $('#editorMode').textContent = 'NEW ARTICLE';
  $('#postStatusBadge').textContent = 'DRAFT';
  $('#postStatusBadge').className = 'post-status';
  $('#deletePost').hidden = true;
  $('#saveDraft').textContent = 'Save as draft';
  $('#publishPost').textContent = 'Publish article ↗';
  $$('.admin-row').forEach((row) => row.classList.remove('selected'));
}

function populateEditor(post) {
  state.selectedSlug = post.slug;
  state.selectedStatus = post.status;
  const form = $('#editorForm');
  for (const name of ['title', 'summary', 'body', 'category', 'author', 'cover_image']) form.elements[name].value = post[name] || '';
  form.elements.tags.value = (post.tags || []).join(', ');
  form.elements.featured.checked = Boolean(post.featured);
  $('#editorMode').textContent = `EDITING / ${post.slug}`;
  $('#postStatusBadge').textContent = post.status.toUpperCase();
  $('#postStatusBadge').className = `post-status ${post.status === 'published' ? 'published' : ''}`;
  $('#deletePost').hidden = false;
  $('#saveDraft').textContent = post.status === 'published' ? 'Unpublish to draft' : 'Save as draft';
  $('#publishPost').textContent = post.status === 'published' ? 'Update published ↗' : 'Publish article ↗';
  $$('.admin-row').forEach((row) => row.classList.toggle('selected', row.dataset.slug === post.slug));
}

async function connectToken() {
  const input = $('#tokenInput');
  const candidate = input.value.trim();
  if (!candidate) { toast('Paste the admin token first.', true); return; }
  state.token = candidate;
  input.value = '';
  try {
    await api('GET', '/admin/posts?limit=1', { auth: true });
    $('#tokenStatus').textContent = 'Connected. Your token stays in this tab and is never stored.';
    $('#connectToken').hidden = true;
    $('#tokenInput').hidden = true;
    $('#disconnectToken').hidden = false;
    toast('Editor connected.');
    await loadAdminPosts();
  } catch (error) {
    state.token = '';
    toast(error.message, true);
  }
}

function disconnectToken() {
  state.token = '';
  resetEditor();
  $('#adminList').textContent = 'Connect to load posts.';
  $('#tokenStatus').textContent = 'Enter the admin token to work with drafts. It stays in this tab only.';
  $('#connectToken').hidden = false;
  $('#tokenInput').hidden = false;
  $('#disconnectToken').hidden = true;
  toast('Editor disconnected.');
}

async function loadAdminPosts() {
  const list = $('#adminList');
  if (!state.token) { list.textContent = 'Connect to load posts.'; return; }
  const params = new URLSearchParams({ limit: '50' });
  if ($('#adminStatusFilter').value) params.set('status', $('#adminStatusFilter').value);
  try {
    const { data } = await api('GET', `/admin/posts?${params}`, { auth: true });
    list.replaceChildren();
    if (!data.posts.length) { const empty = document.createElement('p'); empty.className = 'list-empty'; empty.textContent = 'No posts in this view yet.'; list.append(empty); return; }
    data.posts.forEach((post) => {
      const row = document.createElement('button');
      row.type = 'button';
      row.className = `admin-row ${post.slug === state.selectedSlug ? 'selected' : ''}`;
      row.dataset.slug = post.slug;
      const title = document.createElement('span');
      title.className = 'admin-row-title';
      title.textContent = post.title;
      const meta = document.createElement('span');
      meta.className = 'admin-row-meta';
      const category = document.createElement('span');
      category.textContent = post.category;
      const status = document.createElement('span');
      status.className = post.status === 'published' ? 'published' : '';
      status.textContent = post.status.toUpperCase();
      meta.append(category, status);
      row.append(title, meta);
      row.addEventListener('click', () => loadEditorPost(post.slug));
      list.append(row);
    });
    if (data.total > 50) { const note = document.createElement('p'); note.className = 'list-empty'; note.textContent = `Showing 50 of ${data.total}. Use the API console for later pages.`; list.append(note); }
  } catch (error) { toast(error.message, true); }
}

async function loadEditorPost(slug) {
  try {
    const { data } = await api('GET', `/admin/posts/${encodeURIComponent(slug)}`, { auth: true });
    populateEditor(data);
  } catch (error) { toast(error.message, true); }
}

function editorPayload(status) {
  const form = $('#editorForm');
  const values = new FormData(form);
  return {
    title: String(values.get('title') || '').trim(),
    summary: String(values.get('summary') || '').trim(),
    body: String(values.get('body') || '').trim(),
    category: String(values.get('category') || '').trim(),
    author: String(values.get('author') || '').trim(),
    cover_image: String(values.get('cover_image') || '').trim(),
    tags: String(values.get('tags') || '').split(',').map((tag) => tag.trim()).filter(Boolean),
    featured: form.elements.featured.checked,
    status,
  };
}

async function savePost(status) {
  if (!state.token) { toast('Connect the editor token first.', true); return; }
  if (!$('#editorForm').reportValidity()) return;
  const payload = editorPayload(status);
  const method = state.selectedSlug ? 'PATCH' : 'POST';
  const path = state.selectedSlug ? `/admin/posts/${encodeURIComponent(state.selectedSlug)}` : '/admin/posts';
  try {
    const { data } = await api(method, path, { auth: true, body: payload });
    populateEditor(data);
    toast(status === 'published' ? 'Article published.' : 'Draft saved.');
    await Promise.all([loadAdminPosts(), loadPosts(), loadSummary()]);
  } catch (error) { toast(error.message, true); }
}

async function deletePost() {
  if (!state.selectedSlug || !state.token) return;
  if (!confirm('Delete this post permanently?')) return;
  try {
    await api('DELETE', `/admin/posts/${encodeURIComponent(state.selectedSlug)}`, { auth: true });
    resetEditor();
    toast('Post deleted.');
    await Promise.all([loadAdminPosts(), loadPosts(), loadSummary()]);
  } catch (error) { toast(error.message, true); }
}

async function sendConsoleRequest(event) {
  event.preventDefault();
  const method = $('#requestMethod').value;
  const path = $('#requestPath').value.trim();
  let body;
  if ((method === 'POST' || method === 'PATCH') && $('#requestBody').value.trim()) {
    try { body = JSON.parse($('#requestBody').value); }
    catch { toast('The request body must be valid JSON.', true); return; }
  }
  try {
    const result = await api(method, path, { body, auth: $('#requestAuth').checked });
    showResponse(result);
  } catch (error) {
    if (error instanceof APIError) showResponse(error.result);
    else { showResponse({ status: 'ERR', data: { error: error.message }, duration: 0, cache: '—', ok: false }); toast(error.message, true); }
  }
}

function bindEvents() {
  $$('[data-nav]').forEach((button) => button.addEventListener('click', () => navigate(button.dataset.nav)));
  $$('a[href="#latest"], a[href="#browse"]').forEach((link) => link.addEventListener('click', (event) => {
    event.preventDefault();
    if (location.pathname.startsWith('/stories/')) {
      location.assign(`/${link.getAttribute('href')}`);
      return;
    }
    navigate('explore');
    setTimeout(() => $(link.getAttribute('href')).scrollIntoView({ behavior: 'smooth' }), 0);
  }));
  $('#refreshPublic').addEventListener('click', () => { health(); loadSummary(); loadPosts(); });
  $('#filters').addEventListener('submit', (event) => event.preventDefault());
  let searchTimer;
  $('#searchInput').addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { state.page = 0; loadPosts(); }, 280); });
  ['categoryFilter', 'tagFilter', 'featuredFilter'].forEach((id) => $(`#${id}`).addEventListener('change', () => { state.page = 0; loadPosts(); }));
  $('#previousPage').addEventListener('click', () => { if (state.page > 0) { state.page--; loadPosts(); } });
  $('#nextPage').addEventListener('click', () => { if ((state.page + 1) * 9 < state.total) { state.page++; loadPosts(); } });
  $('#connectToken').addEventListener('click', connectToken);
  $('#tokenInput').addEventListener('keydown', (event) => { if (event.key === 'Enter') connectToken(); });
  $('#disconnectToken').addEventListener('click', disconnectToken);
  $('#refreshAdmin').addEventListener('click', loadAdminPosts);
  $('#adminStatusFilter').addEventListener('change', loadAdminPosts);
  $('#newPost').addEventListener('click', resetEditor);
  $('#editorForm').addEventListener('submit', (event) => event.preventDefault());
  $('#saveDraft').addEventListener('click', () => savePost('draft'));
  $('#publishPost').addEventListener('click', () => savePost('published'));
  $('#deletePost').addEventListener('click', deletePost);
  $('#requestForm').addEventListener('submit', sendConsoleRequest);
}

bindEvents();
resetEditor();
$('#today').textContent = new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
health();
const articleMatch = location.pathname.match(/^\/stories\/([^/]+)$/);
if (articleMatch) {
  loadArticlePage(articleMatch[1]);
} else {
  if (location.hash === '#editor' || location.hash === '#console') navigate(location.hash.slice(1));
  loadSummary();
  loadPosts();
}
