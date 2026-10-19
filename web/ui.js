'use strict';
// Shared UI primitives. Values from the router are inserted as text, never HTML.
const $ = id => document.getElementById(id);

function el(tag, className, text) {
  const n = document.createElement(tag);
  if (className) n.className = className;
  if (text !== undefined) n.textContent = text;
  return n
}
function draftFields(inputs) {
  let changed = false,
    session = csrf;
  for (const input of inputs) input.addEventListener('input', () => {
    changed = true;
    session = csrf
  });
  return {
    saved() {
      changed = false;
      session = csrf
    },
    sync(render) {
      if (session !== csrf) {
        changed = false;
        session = csrf
      }
      if (!changed) render()
    }
  };
}

function box(parent, title, description) {
  const n = el('div', 'panel-box');
  n.append(el('h2', '', title));
  if (description) n.append(el('p', 'muted', description));
  parent.append(n);
  return n
}

function button(parent, label, fn, kind = 'secondary') {
  const b = el('button', kind, label);
  b.type = 'button';
  b.onclick = () => action(b, fn);
  parent.append(b);
  return b
}

function output(parent) {
  const n = el('div', 'advanced-result');
  parent.append(n);
  return n
}

function select(parent, choices, label) {
  const wrap = el('label', '', label),
    n = el('select');
  n.setAttribute('aria-label', label);
  for (const [value, text] of choices) {
    const o = el('option', '', text);
    o.value = value;
    n.append(o)
  }
  wrap.append(n);
  parent.append(wrap);
  return n
}

function field(parent, label, type = 'text') {
  const wrap = el('label', '', label),
    n = el('input');
  n.type = type;
  if (type === 'password') {
    n.autocomplete = 'current-password';
    n.dataset.secret = 'true'
  }
  wrap.append(n);
  parent.append(wrap);
  return n
}

function check(parent, label) {
  const wrap = el('label', 'quic-label'),
    n = el('input');
  n.type = 'checkbox';
  wrap.append(n, document.createTextNode(label));
  parent.append(wrap);
  return n
}

function showRows(node, rows, columns) {
  node.replaceChildren();
  const wrap = el('div', 'table-scroll'),
    table = el('table', 'advanced-table'),
    head = el('tr');
  for (const [, label] of columns) head.append(el('th', '', label));
  const thead = el('thead');
  thead.append(head);
  table.append(thead);
  const body = el('tbody');
  for (const row of rows) {
    const tr = el('tr');
    for (const [key] of columns) tr.append(el('td', '', row[key] ?? '—'));
    body.append(tr)
  }
  table.append(body);
  wrap.append(table);
  node.append(wrap);
  if (!rows.length) node.append(el('p', 'muted', 'Пока нет данных.'))
}

