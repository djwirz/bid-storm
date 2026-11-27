# What the recording proxy costs

Spec §5 asks for the proxy's cost to be measured by running with and without it. Measured on 2026-11-27.

## Method
`targets/holdfast/proxy-cost.sh`: five pairs of storms (one with the proxy, one without, order alternating) on the same schedule, under the default fake settings, then five more against a fake that answers at once and never fails (`fake-quick.env`). The local stack is restarted before every run and warmed with a short storm that is thrown away. The number compared is each bid POST's reply time, counted from its intended send. A run counts only if it is valid, the target's checker passed and the shared fake holds its payments.

Generator, proxy, target (two instances), fake, database and subscribers shared one 8-core laptop, which was busy: load average around 8, much of it the test itself.

## Result
**No cost that these runs could tell apart from run-to-run noise.**

Reply time with the proxy minus without it, per pair, in whole milliseconds:

| Fake | Usable pairs | Typical reply, middle pair | Typical reply, range | Slow reply (9 in 10 faster), middle pair | Slow reply, range |
|---|---|---|---|---|---|
| Waits 150 to 500 ms | 4 | about 15 slower | 16 faster to 28 slower | about 6 faster | 15 faster to 20 slower |
| Answers at once | 5 | no difference | 4 faster to under 1 slower | about 2 slower | 6 faster to 6 slower |

- Under the default settings the fake's random wait swamps anything the extra hop could add. These runs can't say what it adds there.
- Against the quick fake, a typical reply took about 17 ms with or without the proxy. A cost of more than a few milliseconds on a typical reply would probably have shown.
- The slowest one reply in a hundred swung by tens of milliseconds in both directions and says nothing.
- One run under the default settings was invalid (a send went out later than the 50 ms limit), so its pair is left out.

The twenty runs behind the table are in [`runs/curated/proxy-cost/`](../runs/curated/proxy-cost/), with each run's figures in `report.md`.

## What this doesn't show
- Anything about a loaded or deployed setup, or higher bid rates.
- The time each card call spends in the proxy: without the proxy nothing records card calls, so only the bidder's reply time can be compared.
- A cost smaller than the noise. Five pairs is a small sample.
- Without the proxy the target calls the fake directly, so this is the cost of the hop as used here, not of recording alone.

## A first attempt that was wrong
The first twenty runs are not reported. The script set `FAKE_PAYMENTS_URL` to an empty string for the runs without the proxy, where it needed to be unset. Holdfast reads empty as "no shared fake" and used a private one with its own 150 to 500 ms wait and no faults. Those runs were marked valid, because validity is about the recording, not about what the target did. It showed up as a quick-fake run without the proxy taking over 200 ms. The summary now rejects a run the shared fake holds nothing for.
