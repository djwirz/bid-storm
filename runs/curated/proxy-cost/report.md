## fake waits 150-500 ms

| Run | Proxy | typical ms | slow ms | slowest 1% ms |
|---|---|---|---|---|
| 20261127T150855.615Z-s1 | on | 257.6 | 484.4 | 524.2 |
| 20261127T151015.175Z-s1 | off | 233.2 | 489.9 | 563.8 |
| 20261127T151134.794Z-s1 | off | 248.1 | 485.4 | 526.0 |
| 20261127T151258.051Z-s1 | on | 252.8 | 478.5 | 536.4 |
| 20261127T151417.508Z-s1 | on | 259.8 | 496.5 | 588.6 |
| 20261127T151540.587Z-s1 | off | 232.3 | 476.3 | 520.8 |
| 20261127T151659.175Z-s1 | off | 245.7 | 475.2 | 526.9 |
| 20261127T151817.783Z-s1 | on | 230.3 | 459.9 | 533.5 |
| 20261127T151941.633Z-s1 (invalid) | on | 294.8 | 528.6 | 982.0 |
| 20261127T152104.515Z-s1 | off | 225.4 | 465.2 | 514.6 |

Proxy on minus off, over 4 usable pairs:

- typical: median +14.6 ms, from -15.5 to +27.5; higher with the proxy in 3 of 4
- slow: median -6.2 ms, from -15.3 to +20.2; higher with the proxy in 1 of 4
- slowest 1%: median +8.5 ms, from -39.6 to +67.8; higher with the proxy in 3 of 4

## fake waits 0 ms

| Run | Proxy | typical ms | slow ms | slowest 1% ms |
|---|---|---|---|---|
| 20261127T152152.806Z-s1 | on | 17.3 | 40.9 | 61.5 |
| 20261127T152212.806Z-s1 | off | 17.0 | 38.9 | 58.3 |
| 20261127T152232.731Z-s1 | off | 17.0 | 38.6 | 76.5 |
| 20261127T152252.671Z-s1 | on | 17.4 | 44.2 | 106.8 |
| 20261127T152313.481Z-s1 | on | 17.4 | 44.4 | 82.3 |
| 20261127T152333.762Z-s1 | off | 17.4 | 41.0 | 64.0 |
| 20261127T152353.492Z-s1 | off | 17.0 | 41.6 | 74.9 |
| 20261127T152413.229Z-s1 | on | 16.8 | 35.3 | 48.2 |
| 20261127T152432.854Z-s1 | on | 15.6 | 42.4 | 81.1 |
| 20261127T152452.941Z-s1 | off | 19.2 | 42.1 | 92.3 |

Proxy on minus off, over 5 usable pairs:

- typical: median +0.0 ms, from -3.6 to +0.4; higher with the proxy in 3 of 5
- slow: median +2.1 ms, from -6.2 to +5.6; higher with the proxy in 4 of 5
- slowest 1%: median +3.3 ms, from -26.7 to +30.3; higher with the proxy in 3 of 5
