package main

// resolveModelID is the model-selection analogue of resolveProviderName:
// --model wins, then GROK_MODEL, then providerDefault (the resolved
// provider's own configured Model, passed in by the caller since "the
// default" is per-provider, not global). This is the only model-selection
// override surface — a model's other properties (context window, sampling
// defaults, API backend) live in the provider's Models catalog, keyed by
// whatever id this function resolves to (see settings.ProviderConfig.ModelInfo).
func resolveModelID(flagValue string, getenv func(string) string, providerDefault string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := getenv("GROK_MODEL"); v != "" {
		return v
	}
	return providerDefault
}
