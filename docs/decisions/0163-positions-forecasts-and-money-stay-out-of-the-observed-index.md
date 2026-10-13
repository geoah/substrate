---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0163. Positions, forecasts and money stay out of the observed index

## Context and problem statement

Positions, forecasts and money look like observed numbers and break what the
index assumes of one. Version 1 of the plan listed `latitude` and `longitude`
as gauge metrics, which stores them and fails every question asked of a
track. A forecast's value is about a time after it was issued, and every
reducer assumes measured values. A currency looks like a unit. Each is cheap
to keep out now and costly to take out of an index that holds it
([the plan, section 5.5](../plans/observations.md)).

## Considered options

- All three out of the index: positions wait for a `geopoint` datatype and a
  packed `track`, forecasts stay in their provider's mirror, money stays a
  `money` property on a typed kind
- Positions as two gauge metrics in degrees
- Forecasts in the index, marked by an `issuedAt` before their time
- Money as a quantity, with each currency a unit

## Decision outcome

Chosen: all three out of the index.

- **Positions are not quantities.** A mean of latitudes is wrong across the
  antimeridian, distance is geodesic, and the useful filters are spatial. A
  `geopoint` datatype (`{lat, lon, alt?, accuracy?}`, WGS84) and a packed
  `track`, whose managed stats are a bounding box, a length and a duration,
  are the only way positions will enter the index, in a later phase. Until
  then positions stay inside workout and vehicle mirrors.
- **Forecasts never enter the index.** Home Assistant's `measurement` class
  excludes "a prediction of the future" for the same reason. Forecasts live
  in their provider's mirror. `issuedAt` on an observation records when a
  measured result became known (a late lab result, a vendor recomputation);
  it does not make a forecast an observation.
- **Money is never a unit**, and a currency is not a measure
  ([0153](0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md)):
  an exchange rate is a market observation with a date, not a definition. A
  balance or a price is a typed kind with a `money` property
  ([0140](0140-money-is-an-integer-amount-of-its-currencys-minor-unit.md)),
  aggregated through the aggregate read's `property` selector
  ([0161](0161-the-aggregate-read-is-a-mode-of-the-records-route-checked-before-it-runs.md)).
  A price per unit is money beside a quantity, not a UCUM code.

### Consequences

- Good, because every number in the index can be reduced under its form,
  and no reducer mixes predictions with measurements.
- Good, because money keeps 0140's exactness and its comparison within one
  currency.
- Bad, because "where was I" and "how far did I go" read a workout's own
  fields until `geopoint` and `track` ship.
- Bad, because a chart of a forecast against what happened reads two places.
- Bad, because a balance trend reads `records` through the `property`
  selector, which scans unless a typed expression index exists.
- Bad, because nothing in the engine stops a writer from recording a
  latitude as a gauge in `deg` or a forecast as an observation.

### Confirmation

`TestQuantityRefusesACurrencyAsAUnit` (internal/vocabulary) holds that a
currency code such as `EUR` is no unit a quantity takes. That
the shipped samples declare no latitude or longitude metric and no forecast
kind, and that adapters write neither into the index, is held by review only.

## More information

[The plan, section 5.5](../plans/observations.md) argues all three;
[the implementation brief](../plans/observations/implementation/README.md)
keeps `geopoint` and `track` out of this build and settled money as typed
kinds. Revisit when a consumer needs positions in the index: that is the
`geopoint` and `track` phase, with its own record.
