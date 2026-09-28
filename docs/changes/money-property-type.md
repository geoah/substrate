---
type: feature
---

# A `money` property type holds an amount, a currency and a scale

A kind may declare `type: money`. The value is three members: `amount`, an
integer count of minor units; `currency`, an ISO 4217 code; and `decimals`,
how many of the amount's digits follow the decimal point. This record stores
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
      decimals: 2
```

A filter operand is a money value, and the comparison holds within its
currency: `{"price": {"lt": {"amount": 20, "currency": "EUR", "decimals":
0}}}` is every EUR price under 20. `orderBy=price` sorts by the exact number.
`min` and `max` bound that number. The console shows the value in the
reader's locale (`€19.99`) and edits it as the text `19.99 EUR`.
