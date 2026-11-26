// Small DOM helpers. Text is always set as text, never parsed as HTML.

export function element(id: string): HTMLElement {
  const found = document.getElementById(id);
  if (!found) throw new Error(`no #${id}`);
  return found;
}

export function node<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  text = '',
  className = '',
): HTMLElementTagNameMap[K] {
  const made = document.createElement(tag);
  made.textContent = text;
  if (className) made.className = className;
  return made;
}

export function table(caption: string, head: string[], rows: string[][]): HTMLTableElement {
  const made = node('table');
  made.append(node('caption', caption));
  const header = made.createTHead().insertRow();
  for (const name of head) {
    const cell = node('th', name);
    cell.scope = 'col';
    header.append(cell);
  }
  const body = made.createTBody();
  for (const row of rows) {
    const line = body.insertRow();
    for (const value of row) line.insertCell().textContent = value;
  }
  return made;
}
