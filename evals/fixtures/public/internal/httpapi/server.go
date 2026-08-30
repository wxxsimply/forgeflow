package httpapi

import fixture "forgeflow-eval-fixture"

func CheckCSRF(endpoint, token string) bool { return fixture.CheckCSRF(endpoint, token) }
