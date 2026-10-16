package nozle

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The SDK's response structs must not declare JSON keys the API schema does not have.
// testdata/openapi_schemas.json is written by scripts/sync_openapi_contract.py from
// nozle-openapi.yaml.
func TestResponseStructsMatchTheOpenAPISchemas(t *testing.T) {
	raw, err := os.ReadFile("testdata/openapi_schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemas map[string]struct {
		Properties []string `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schemas); err != nil {
		t.Fatal(err)
	}
	contract := []struct {
		value   any
		schemas []string
	}{
		{UsageResult{}, []string{"CreditsUsageCheckResponse", "CreditsUsageTrackResponse"}},
		{CanResult{}, []string{"PlatformCanResponse"}},
		{PingResult{}, []string{"PlatformPingResponse"}},
		{CostEventResult{}, []string{"CostEventAccepted"}},
		{CreditBalance{}, []string{"CreditsCustomerBalance"}},
		{CreditBalances{}, []string{"CreditsCustomerBalanceList"}},
		{CreditOperationPage{}, []string{"CreditsCustomerOperationPage", "CreditsEntityOperationPage"}},
		{Entity{}, []string{"CreditsEntity"}},
		{EntityPage{}, []string{"CreditsEntityPage"}},
		{EntityCreditBalance{}, []string{"CreditsEntityBalance"}},
		{EntityCreditBalances{}, []string{"CreditsEntityBalanceList"}},
		{SubscriptionOptions{}, []string{"BillingSubscriptionOptionsResponse"}},
		{CustomerUsage{}, []string{"CustomerUsageObject"}},
		{CustomerProjectedUsage{}, []string{"CustomerProjectedUsageObject"}},
		{ChargeUsage{}, []string{"CustomerChargeUsageObject"}},
	}
	for _, c := range contract {
		allowed := map[string]bool{}
		for _, name := range c.schemas {
			schema, ok := schemas[name]
			if !ok {
				t.Fatalf("schema %s missing from testdata/openapi_schemas.json", name)
			}
			for _, p := range schema.Properties {
				allowed[p] = true
			}
		}
		var unknown []string
		for _, key := range jsonKeys(reflect.TypeOf(c.value)) {
			if !allowed[key] {
				unknown = append(unknown, key)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("%T declares keys the API never sends: %v", c.value, unknown)
		}
	}
}

func jsonKeys(typ reflect.Type) []string {
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if field.Anonymous && tag == "" {
			keys = append(keys, jsonKeys(field.Type)...)
			continue
		}
		if tag != "" && tag != "-" && field.IsExported() {
			keys = append(keys, tag)
		}
	}
	return keys
}
