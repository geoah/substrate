---
type: feature
---

# A `money` property type holds an amount and its currency

A kind may declare `type: money`. The value is two members: `amount`, an
integer count of minor units, and `currency`, an active ISO 4217 code whose
minor unit places the decimal point (2 for EUR, 0 for JPY). This record stores
a price of 19.99 EUR:

```yaml
kind: example.com/shop/item
metadata:
  id: coffee
data:
  properties:
    price:
      amount: 1999
      currency: EUR
```

A filter operand is a money value, and the comparison holds within its
currency: `{"price": {"lt": {"amount": 2000, "currency": "EUR"}}}` is every
EUR price under 20.00. `orderBy=price` sorts by the exact number. `min` and
`max` bound that number. The console shows the value in the reader's locale
(`€19.99`) and edits it as an amount beside a currency picker.
