# bid-storm

A load generator that records every stage of a bid storm against a real-time auction, and a data-deep static site built from those recordings. First target: [Holdfast](https://github.com/djwirz/holdfast), where a bid counts only once a card hold covers it.

The question the data should answer on sight: many bids land at once, they get one agreed order, and none counts without a hold.

**Status:** planning. Design: [docs/spec.md](docs/spec.md) and [docs/adr/](docs/adr/). Synthetic users and auctions only, against local deployments.
