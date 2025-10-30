## Contribution and Review Checklist (paymentnew module)

### Must follow

- Access state only via `PaymentStateMachine` APIs:
  - Read: `LoadState(userID, appID, productID)` (loads from memory, falls back to Redis, rehydrates memory)
  - Write: `SaveState(state)` (writes to Redis and syncs memory)
  - Delete: `DeleteState(userID, appID, productID)` (deletes from Redis and memory)
- Do NOT read/write Redis directly or introduce side caches for payment state.
- For state transitions, prefer `processEvent(...)` + `updateState(...)` to ensure atomic updates, timestamps, and persistence.
- After sending “payment required” notification, update `PaymentStatus=notification_sent` accordingly.
- When developer VC returns with `code==0`, you must:
  - Set `PaymentStatus=developer_confirmed`
  - Persist purchase receipt via `storePurchaseInfo`
  - Notify frontend purchase completion (`notifyFrontendPurchaseCompleted`)

### Recommended practices

- Add timeout, backoff, and observability (metrics/logs) to polling and network calls.
- Log return codes and duration for external calls (DID gate / developer service).
- Validate `X-Forwarded-Host` or provide configuration-based fallback for nonstandard domains.


