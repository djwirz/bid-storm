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

/**
 * Lets the keyboard reach every box that scrolls sideways, naming it by the heading above it.
 * Boxes filled later are covered: the attributes sit on the box, not its contents.
 */
export function focusableScrollBoxes(): void {
  for (const box of document.querySelectorAll<HTMLElement>('.scroll')) {
    const section = box.closest('section, article');
    const heading = section?.querySelector('h2, h3')?.textContent ?? 'Wide content';
    box.tabIndex = 0;
    box.setAttribute('role', 'group');
    box.setAttribute('aria-label', `${heading}: scrolls sideways`);
  }
}
