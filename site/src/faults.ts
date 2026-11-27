// One card per kind of fault: what was injected, how often it showed, a real case step by step,
// and what it did not do. Every count and claim is worked out from the recording.

import { examples, heldFor, millis, type Example, type Fault, type Storm } from './data.ts';
import { element, node, table } from './dom.ts';

const rate = (value: string | number | undefined) => {
  const n = Number(value);
  return n > 0 ? `about ${Math.round(n * 100)} in 100` : 'an undeclared share of';
};

interface Card {
  fault: Fault;
  title: string;
  injected: (storm: Storm) => string;
  after: (storm: Storm, cases: Example[]) => string;
}

const count = (cases: Example[], test: (c: Example) => boolean) => cases.filter(test).length;
const noHold = (c: Example) => c.bid !== null && heldFor(c.bid).placed === null && c.bid.status !== 'accepted';

export const CARDS: Card[] = [
  {
    fault: 'declined',
    title: 'The card is declined',
    injected: (s) => `By the payment fake, for ${rate(s.run.fake_settings_declared['FAKE_DECLINE_RATE'])} holds.`,
    after: (_, cases) => `${count(cases, noHold)} of ${cases.length} ended with no hold and were not accepted.`,
  },
  {
    fault: 'provider',
    title: 'The provider errors and nothing is held',
    injected: (s) =>
      `By the fake, on every ${s.run.fake_settings_declared['FAKE_SERVER_ERROR_EVERY'] ?? '?'}th new hold. ` +
      'When a hold had already been placed, the fake recovers by itself and answers as for success, so those ' +
      'can’t be told apart from outside and aren’t counted.',
    after: (_, cases) => `${count(cases, noHold)} of ${cases.length} ended with no hold and were not accepted.`,
  },
  {
    fault: 'lost',
    title: 'The card is held but the answer is lost',
    injected: (s) =>
      `By the fake, for ${rate(s.run.fake_settings_declared['FAKE_LOST_RESPONSE_RATE'])} holds.`,
    after: (_, cases) => {
      const released = count(cases, (c) => c.bid !== null && heldFor(c.bid).released !== null);
      return (
        `The target repeated the request with the same key. ${released} of ${cases.length} holds were then released. ` +
        'The audit check found no bid with a second hold.'
      );
    },
  },
  {
    fault: 'release',
    title: 'Releasing a hold is refused for now',
    injected: (s) =>
      `By the fake, as a rate limit on ${rate(s.run.fake_settings_declared['FAKE_CANCEL_FAULT_RATE'])} release calls.`,
    after: (_, cases) => {
      const waits = cases.flatMap((c) => {
        const refused = c.bid?.holds.find((h) => h.op === 'cancel' && h.ok === false);
        const released = c.bid ? heldFor(c.bid).released : null;
        return refused && released !== null ? [released - refused.replied] : [];
      });
      const longest = waits.length ? `, the longest ${millis(Math.max(...waits))} after the first refusal` : '';
      return `${waits.length} of ${cases.length} holds were released in the end${longest}.`;
    },
  },
  {
    fault: 'retried',
    title: 'A bid is sent twice',
    injected: (s) => `By the generator: ${rate(s.run.retry_rate)} sends reuse an earlier send’s key.`,
    after: (storm, cases) => {
      const original = storm.other_sends.filter((s) => s.bid !== null && s.bid === s.retry_of).length;
      return `${original} of ${cases.length} got back the bid their first send had made. The audit check found one hold at most for each bid.`;
    },
  },
  {
    fault: 'conflict',
    title: 'A key is reused for a different amount',
    injected: (s) => `By the generator: ${rate(s.run.conflict_rate)} of its retries raise the amount.`,
    after: (_, cases) =>
      `${count(cases, (c) => c.steps.some((x) => x.what.includes('idempotency_conflict')))} of ${cases.length} were ` +
      'refused as a conflict and made no bid.',
  },
];

function card(storm: Storm, { fault, title, injected, after }: Card): HTMLElement {
  const cases = examples(storm, fault);
  const made = node('article', '', 'fault');
  made.id = `fault-${fault}`;
  made.append(node('h3', title), node('p', injected(storm), 'note'), node('p', `Seen ${cases.length} times.`, 'seen'));
  if (cases.length === 0) return made;
  made.append(node('p', after(storm, cases)));

  const shown = node('div', '', 'scroll');
  const position = node('output');
  position.setAttribute('aria-live', 'polite');
  let current = 0;
  const show = (index: number) => {
    current = (index + cases.length) % cases.length;
    const example = cases[current]!;
    position.textContent = `Example ${current + 1} of ${cases.length}`;
    const start = example.steps[0]!.at;
    shown.replaceChildren(
      table(
        example.title,
        ['After', 'What happened'],
        example.steps.map((step) => [step.at === start ? '0' : millis(step.at - start), step.what]),
      ),
    );
  };
  const button = (text: string, step: number) => {
    const b = node('button', text);
    b.type = 'button';
    b.setAttribute('aria-label', `${text} example: ${title}`);
    b.addEventListener('click', () => show(current + step));
    return b;
  };
  const controls = node('div', '', 'controls');
  controls.append(button('Previous', -1), button('Next', 1), position);
  const details = node('details');
  details.append(node('summary', 'Step through a real one'), controls, shown);
  made.append(details);
  show(0);
  return made;
}

export function faults(storm: Storm): void {
  element('faults').replaceChildren(...CARDS.map((c) => card(storm, c)));
}
