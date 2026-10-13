package controlleddev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"forgeflow/internal/model"
)

const (
	MaxPromptBytes     = 128 * 1024
	MaxOutputTokens    = 16_000
	MaximumBalanceCNY  = 5
	deepSeekBalanceURL = "https://api.deepseek.com/user/balance"
)

// Provider refuses an oversized paid request and checks the dedicated
// DeepSeek account balance before each call. The account must have no automatic
// recharge or other consumers; this wrapper cannot control provider billing.
type Provider struct {
	inner      model.Provider
	apiKey     string
	client     *http.Client
	balanceURL string
}

func NewProvider(inner model.Provider, apiKey string) (*Provider, error) {
	if inner == nil || inner.Name() != "deepseek" || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("controlled development requires a DeepSeek provider and a private API key")
	}
	return &Provider{inner: inner, apiKey: strings.TrimSpace(apiKey), client: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, balanceURL: deepSeekBalanceURL}, nil
}

func (p *Provider) Name() string { return p.inner.Name() }

func (p *Provider) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	if request.Model != "deepseek-flash" || request.MaxOutputTokens <= 0 || request.MaxOutputTokens > MaxOutputTokens ||
		len(request.Instructions)+len(request.Input)+len(request.ResponseFormat.Schema) > MaxPromptBytes {
		return model.Response{}, &model.Error{Kind: model.ErrorInvalidRequest, Message: "controlled model request exceeds the allowlist or size limits"}
	}
	if err := p.checkBalance(ctx); err != nil {
		return model.Response{}, err
	}
	return p.inner.Generate(ctx, request)
}

func (p *Provider) checkBalance(ctx context.Context) error {
	parsed, err := url.Parse(p.balanceURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && parsed.Hostname() == "127.0.0.1")) {
		return &model.Error{Kind: model.ErrorInvalidRequest, Message: "controlled balance endpoint is invalid"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.balanceURL, nil)
	if err != nil {
		return &model.Error{Kind: model.ErrorInvalidRequest, Message: "controlled balance request could not be created"}
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return &model.Error{Kind: model.ErrorTransient, Message: "DeepSeek balance could not be verified"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &model.Error{Kind: model.ErrorAuthentication, StatusCode: resp.StatusCode, Message: "DeepSeek balance preflight failed"}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return &model.Error{Kind: model.ErrorInvalidOutput, Message: "DeepSeek balance response is invalid"}
	}
	var balance struct {
		Available bool `json:"is_available"`
		Infos     []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if json.Unmarshal(data, &balance) != nil || !balance.Available || len(balance.Infos) == 0 {
		return &model.Error{Kind: model.ErrorInvalidOutput, Message: "DeepSeek balance is unavailable"}
	}
	total := new(big.Rat)
	for _, info := range balance.Infos {
		amount, ok := new(big.Rat).SetString(info.Total)
		if !ok || amount.Sign() < 0 {
			return &model.Error{Kind: model.ErrorInvalidOutput, Message: "DeepSeek balance amount is invalid"}
		}
		if info.Currency != "CNY" {
			if amount.Sign() != 0 {
				return &model.Error{Kind: model.ErrorInvalidOutput, Message: "controlled account must have only a CNY balance"}
			}
			continue
		}
		total.Add(total, amount)
	}
	if total.Sign() <= 0 || total.Cmp(big.NewRat(MaximumBalanceCNY, 1)) > 0 {
		return &model.Error{Kind: model.ErrorInvalidRequest, Message: "controlled account balance must be between 0 and 5 CNY"}
	}
	return nil
}
