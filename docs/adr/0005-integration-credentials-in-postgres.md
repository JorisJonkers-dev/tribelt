# Integration credentials entered in the UI are sealed in Postgres, not written to Vault

An admin connects Google Search Console, Bing Webmaster Tools, IndexNow and Cloudflare analytics on the Integrations page in `/stats`, without touching Vault. The credential has to be stored somewhere the running binary can read. Writing it to Vault would keep every secret in one place, but the pod's Vault identity would then need write access to its own path, the estate's secrets would gain a second writer besides the operators, and a credential would only reach the process after an External Secrets sync and a restart. Instead the binary seals each credential with AES-256-GCM and stores it in the `integrations` table of `tribelt_db`, which the existing daily `pg_dumpall` already backs up.

The key is derived with HKDF-SHA256 from `SESSION_KEY` (info `tribelt-integrations-v1`), which already arrives from Vault, so no new secret is needed. Each row records a key id derived from the same secret, and the integration kind is bound into the ciphertext as associated data, so a blob cannot be moved to another row. Secrets are write-only: the UI shows a fingerprint (the first 12 hex characters of the credential's SHA-256), who saved it and when, never the value. Every change, test, sync and IndexNow submission is appended to `integration_events`.

The Vault-delivered variables (`GSC_SERVICE_ACCOUNT_JSON`, `GSC_SITE_URL`, `BING_API_KEY`, `BING_SITE_URL`) keep working as a read-only source shown as "managed by Vault"; a credential saved in the UI takes precedence, and removing it hands back to Vault.

## Consequences

- Rotating `SESSION_KEY` makes every saved credential unreadable: the key id no longer matches, the card shows an error asking to enter the credential again, and nothing is decrypted with the wrong key. Rotating without re-entry needs a one-off re-encryption (open with the old key, seal with the new) before the switch; the key id makes the rows to convert easy to find.
- A database dump contains the sealed credentials but not the key, so the dump alone does not reveal them. Anyone holding both `SESSION_KEY` and the dump does.
- `SESSION_KEY` now guards both the session cookie and the stored credentials. Without it (local development) the page still shows Vault-managed sources but cannot save anything.
- Only `ROLE_ADMIN` may change an integration; that role is read from the ID token at sign-in and fixed for the 8-hour session, so a revoked admin keeps it until the session expires.
