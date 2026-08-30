package model

import fixture "forgeflow-eval-fixture"

type ProviderError = fixture.ProviderError

func ShouldRetry(err ProviderError) bool { return fixture.ShouldRetry(err) }
