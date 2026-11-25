# internal/services/connectors/google — the Google connector

**Google's installed-app sign-in on the OAuth engine, whose tokens sign Vertex AI in the user's
Google Cloud project.** The `google` model row lists the Gemini models under Vertex AI's ids.
→ [ADR](../../../../../docs/adr/2026-11-19-the-google-connector-signs-vertex-ai-in-the-users-project.md),
[security record](../../../../../docs/security/2026-11-19-the-google-connector.md).

- **The provider writes Google's endpoints out** (`endpoints`): they span three origins, which
  discovery refuses. The client's secret goes on the exchange and the refresh; no `resource` goes
  anywhere. Every authorize asks `access_type=offline` and `prompt=consent`, so each sign-in
  returns a refresh token. A sign-in without `cloud-platform` is not kept.
- **`Connect` is refused with no client id** (`connectors.ErrNoClient`); `--google-client-id` and
  `--google-client-secret` are empty by default.
- **`FindProject` reads the project as Google's tools do**: `GOOGLE_CLOUD_PROJECT`,
  `CLOUDSDK_CORE_PROJECT`, then the active gcloud configuration's `[core] project`; the location
  is `GOOGLE_CLOUD_LOCATION`, else `global`. `app.New` calls it through `Config.Getenv`, in
  every build, **after the login shell's import**, which carries these variables on macOS.
- **`BaseURL` builds no endpoint from an id that is not well formed**, so the environment cannot
  move the host or the path. With no base the engine signs nothing and the catalog lists no row.
- **The `google` provider is listed whenever the launch found a project**, a Gemini key or not: `gemini` (the Gemini API) and `google` (Vertex AI) are two providers. → [ADR](../../../../../docs/adr/2026-11-20-a-providers-credential-follows-one-order.md).
