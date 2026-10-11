module github.com/nozle-dev/nozle-go/examples/provider-auto

go 1.22.12

require (
	github.com/anthropics/anthropic-sdk-go v1.5.0
	github.com/nozle-dev/nozle-go v0.0.0
	github.com/openai/openai-go/v3 v3.44.0
)

require (
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
)

replace github.com/nozle-dev/nozle-go => ../..
