# Artist Subscriptions

Artist subscriptions automate library growth by periodically polling metadata providers for newly released singles, EPs, and studio albums.

## How Subscriptions Work

1. **Periodic Synchronization:** The backend scheduler checks active artist subscriptions against provider APIs.
2. **Release Detection:** New release IDs are compared against existing library albums and pending download jobs.
3. **Automatic Queueing:** If `auto_download` is enabled, missing releases are queued automatically with low-priority background scheduling.
4. **Import & Export:** Subscriptions can be imported or exported in bulk via standard JSON payload structures.
