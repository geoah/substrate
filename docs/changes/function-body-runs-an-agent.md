---
type: feature
---

# A function body runs an agent granted under `permissions.agents`

A function that names an agent under `permissions.agents` can run it with
`host.agents.call(agent, input)` and read `{reply, thread, status}`. A classifier
no longer needs a staging row and a record trigger to hand a candidate to an
agent. The agent's writes commit
as it runs and stay if the caller fails afterwards, so a delivery that fails
after its agent ran parks instead of retrying, and a keyed call binds its
`Idempotency-Key` to the agent's thread. The function is not delivered the
writes of the agents it grants. The agent runs inside the
caller's `timeout` (at most 60s) and settles its thread when that passes.

```yaml
permissions:
  agents:
    - example.com/commerce/extractorder
timeout: PT60S
```

```python
def main(input, host):
    out = host.agents.call("example.com/commerce/extractorder", input["args"])
    return {"output": {"reply": out["reply"]}}
```
