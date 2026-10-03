# Platform Integration Contract

Module ID `asterisk`; display name `Asterisk`; API prefix `/api/asterisk`; portal UI remote base `/m/asterisk/`; module serves embedded remote under `/ui/`; federation exposes `./routes` and `./nav`.

Use Freya secure runtime and mesh HTTPS, published auth/portal SDKs, readiness-triggered gateway lease/heartbeat registration and graceful deregistration. Register permissions and module roles independently with auth; retry outages without disclosing data before verified authorization. Admin health and scrape listeners follow framework defaults and network policies, not public portal exemptions.

Permissions: `calls:read`, `stats:read`, `registration:read`, `live:read`, `recordings:read`, `dashboard:read`. Module viewer has all except recordings; investigator adds recordings. Seed owner/admin with investigator and operator/member/auditor with viewer; deployment administrators may narrow grants. Recording routes require both call access and recording permission; browser abilities/nav are presentation only, handlers enforce checks.

Every protected route verifies platform user session, revocation and auth permission and then matches tenant to configured binding. SSE and partial-content responses use the same checks. Missing identity -> 401; denied permission/binding -> 403; unknown call -> 404 without confirming foreign ownership. No public PBX data routes. Exporter access is private and restricted to authorized scraping topology.

Manifest routes derive from OpenAPI `x-freya-permission`, with route-specific stream timeout validated against portal SDK bounds. Declare SSE timeout <= supported maximum; client reconnect obtains a new full snapshot. Match route declarations, handlers, nav permissions, CASL abilities and UI exposes in contract validation. No legacy descriptor/menus registration, certificate file polling or metadata-only tenant trust.
