package main

// resolveProviderName is the only provider-selection logic in this
// binary: it picks the --provider flag value if set, else GROK_PROVIDER,
// else empty (meaning "use settings.Config.DefaultProvider"). Everything
// else about a provider — endpoint, model, credential env var — lives in
// the config file's providers: list (see internal/domain/settings), not
// in flags or env vars. Adding a new provider is a config file edit.
func resolveProviderName(flagValue string, getenv func(string) string) string {
	if flagValue != "" {
		return flagValue
	}
	return getenv("GROK_PROVIDER")
}
