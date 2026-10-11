package main

import (
	"context"
	"fmt"
	nozle "github.com/nozle-dev/nozle-go"
)

func main() {
	c := nozle.New("sk_example")
	result, err := c.CheckUsage(context.Background(), nozle.UsageParams{CustomerID: "workspace_123", FeatureCode: "agent_execution", CreditSystemCode: "ai_credits"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(result.Allowed)
}
