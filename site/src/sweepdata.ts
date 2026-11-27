// The shape of data/sweep.json (analysis/src/bidcheck/sweep.py) and how runs are grouped.
// A sweep moves one variable at a time from the base setting.

export type Outcome = 'accepted' | 'too_low' | 'ended' | 'card' | 'other';

export interface SweepRun {
  order: number;
  repeat: number;
  rate: number;
  hold_ms: string;
  decline: number;
  load_before: number;
  load_after: number;
  run_id: string;
  target_commit: string;
  valid: boolean;
  invalid_reasons: string[];
  failed_checks: string[];
  sends: number;
  skipped: number;
  outcomes: Record<Outcome, number>;
  reply_ms: { typical: number | null; slow: number | null };
  outcome_ms: { typical: number | null; slow: number | null };
  peak_in_flight: number;
  send_lag_max_ms: number;
  cpus: number;
}

export const BASE = { rate: 50, hold_ms: '150-500', decline: 0.03 };

export type Variable = keyof typeof BASE;

export const VARIABLES: Record<Variable, { title: string; values: (number | string)[]; label: (v: number | string) => string }> = {
  rate: { title: 'Request rate (bids a second)', values: [10, 25, 50, 100, 200, 300], label: (v) => String(v) },
  hold_ms: {
    title: 'How long a card hold takes (ms)',
    values: ['0', '50-150', '150-500', '500-1500'],
    label: (v) => String(v).replace('-', ' to '),
  },
  decline: {
    title: 'Share of cards declined',
    values: [0, 0.03, 0.2, 0.5],
    label: (v) => `${Math.round(Number(v) * 100)}%`,
  },
};

export interface Setting {
  value: number | string;
  runs: SweepRun[];
}

/** The runs at each value of one variable, with the others at the base setting. */
export function sweepOf(runs: SweepRun[], variable: Variable): Setting[] {
  const others = (Object.keys(BASE) as Variable[]).filter((v) => v !== variable);
  return VARIABLES[variable].values.map((value) => ({
    value,
    runs: runs.filter((r) => r[variable] === value && others.every((o) => r[o] === BASE[o])),
  }));
}

/** The middle value over the valid runs, or null if there are none. Invalid runs never count. */
export function middle(runs: SweepRun[], measure: (r: SweepRun) => number | null): number | null {
  const values = runs
    .filter((r) => r.valid)
    .map(measure)
    .filter((v): v is number => v !== null)
    .sort((a, b) => a - b);
  if (values.length === 0) return null;
  const half = values.length / 2;
  return values.length % 2 ? values[Math.floor(half)]! : (values[half - 1]! + values[half]!) / 2;
}

export const share = (run: SweepRun, outcome: Outcome): number => {
  const total = Object.values(run.outcomes).reduce((a, b) => a + b, 0);
  return total ? (100 * run.outcomes[outcome]) / total : 0;
};
