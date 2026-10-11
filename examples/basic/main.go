package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	nozle "github.com/nozle-dev/nozle-go"
)

func main() {
	if err := run(context.Background(), os.Getenv, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, getenv func(string) string, out io.Writer) error {
	secret := strings.TrimSpace(getenv("NOZLE_SECRET_KEY"))
	if !strings.HasPrefix(secret, "sk_") {
		return fmt.Errorf("NOZLE_SECRET_KEY must contain a server-side sk_ key")
	}
	options := []nozle.Option{}
	if engineURL := strings.TrimSpace(getenv("NOZLE_ENGINE_URL")); engineURL != "" {
		options = append(options, nozle.WithBaseURL(engineURL))
	}
	if coreURL := strings.TrimSpace(getenv("NOZLE_CORE_URL")); coreURL != "" {
		options = append(options, nozle.WithEventsURL(coreURL))
	}
	client := nozle.New(secret, options...)
	if _, err := client.Ping(ctx); err != nil {
		return fmt.Errorf("ping Engine: %w", err)
	}
	plans, err := client.Plans(ctx)
	if err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	systems, err := client.ListCreditSystems(ctx)
	if err != nil {
		return fmt.Errorf("list credit systems from Core: %w", err)
	}
	fmt.Fprintf(out, "connected; plans=%d credit_systems=%d\n", len(plans), len(systems))

	customerID := strings.TrimSpace(getenv("NOZLE_CUSTOMER_ID"))
	featureCode := strings.TrimSpace(getenv("NOZLE_FEATURE_CODE"))
	if customerID == "" && featureCode == "" {
		return nil
	}
	if customerID == "" || featureCode == "" {
		return fmt.Errorf("set both NOZLE_CUSTOMER_ID and NOZLE_FEATURE_CODE for the optional credit check")
	}
	result, err := client.CheckUsage(ctx, nozle.UsageParams{
		CustomerID:       customerID,
		FeatureCode:      featureCode,
		CreditSystemCode: strings.TrimSpace(getenv("NOZLE_CREDIT_SYSTEM_CODE")),
	})
	if err != nil {
		return fmt.Errorf("check configured credit feature: %w", err)
	}
	fmt.Fprintf(out, "credit check: allowed=%t required=%s available=%s\n", result.Allowed, result.CreditsRequired, result.Available)
	return nil
}
