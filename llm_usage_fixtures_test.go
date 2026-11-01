package nozle

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The shared contract of the LLM usage normalizers (the Engine, nozle-js, nozle-python and nozle-go read the same file).
type usageFixtures struct {
	Estimator struct {
		Cases []struct{ Bytes, Tokens int } `json:"cases"`
		Text  []struct {
			Body  map[string]json.RawMessage `json:"body"`
			Bytes int                        `json:"bytes"`
		} `json:"request_text"`
	} `json:"estimator"`
	Usage []struct {
		Name    string         `json:"name"`
		Payload map[string]any `json:"payload"`
		Expect  map[string]any `json:"expect"`
	} `json:"usage"`
	Streams []struct {
		Name          string         `json:"name"`
		SSE           string         `json:"sse"`
		Complete      bool           `json:"complete"`
		DeliveredByte int            `json:"delivered_bytes"`
		Expect        map[string]any `json:"expect"`
	} `json:"streams"`
	Aborted []struct {
		Name         string                     `json:"name"`
		SSE          string                     `json:"sse"`
		RequestBody  map[string]json.RawMessage `json:"request_body"`
		RequestBytes int                        `json:"request_text_bytes"`
		Expect       map[string]any             `json:"expect"`
	} `json:"aborted"`
}

func loadUsageFixtures(t *testing.T) usageFixtures {
	t.Helper()
	raw, err := os.ReadFile("testdata/llm-usage-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures usageFixtures
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func number(t *testing.T, value any) int {
	t.Helper()
	n, ok := value.(json.Number)
	if !ok {
		t.Fatalf("not a number: %v", value)
	}
	i, _ := n.Int64()
	return int(i)
}

var fixtureBreakdowns = []string{"cached_input_tokens", "cache_write_tokens", "cache_write_1h_tokens", "reasoning_tokens", "audio_input_tokens", "audio_output_tokens", "image_input_tokens", "image_output_tokens"}

// checkFixtureUsage compares a detected usage with a fixture's expected totals, breakdowns and cost buckets.
func checkFixtureUsage(t *testing.T, found detectedUsage, want map[string]any) {
	t.Helper()
	if found.format != want["format"] {
		t.Errorf("format = %s, want %v", found.format, want["format"])
	}
	if found.usage.input != number(t, want["input_tokens"]) || found.usage.output != number(t, want["output_tokens"]) {
		t.Errorf("totals = %d/%d, want %v/%v", found.usage.input, found.usage.output, want["input_tokens"], want["output_tokens"])
	}
	props := found.usage.properties()
	for _, key := range fixtureBreakdowns {
		if expected, ok := want[key]; ok {
			if props[key] != number(t, expected) {
				t.Errorf("%s = %v, want %v", key, props[key], expected)
			}
		} else if _, present := props[key]; present {
			t.Errorf("%s = %v, want it left out", key, props[key])
		}
	}
	if tier, ok := want["service_tier"]; ok != (props["service_tier"] != nil) || (ok && tier != props["service_tier"]) {
		t.Errorf("service_tier = %v, want %v", props["service_tier"], tier)
	}
	buckets := map[string]int{}
	for _, cat := range found.usage.categories {
		if cat.tokens > 0 {
			buckets[found.usage.costBucket(cat.name)] = cat.tokens
		}
	}
	wantBuckets := map[string]int{}
	for name, tokens := range want["buckets"].(map[string]any) {
		wantBuckets[name] = number(t, tokens)
	}
	if !reflect.DeepEqual(buckets, wantBuckets) {
		t.Errorf("buckets = %v, want %v", buckets, wantBuckets)
	}
}

func feedFixtureStream(sse string) *streamCollector {
	collector := newStreamCollector()
	for _, line := range strings.Split(sse, "\n") {
		collector.add([]byte(line))
	}
	return collector
}

func TestSharedUsageFixtures(t *testing.T) {
	fixtures := loadUsageFixtures(t)

	t.Run("estimator", func(t *testing.T) {
		for _, c := range fixtures.Estimator.Cases {
			if got := estimateTokens(c.Bytes); got != c.Tokens {
				t.Errorf("estimateTokens(%d) = %d, want %d", c.Bytes, got, c.Tokens)
			}
		}
		for _, c := range fixtures.Estimator.Text {
			if got := requestTextBytes(c.Body); got != c.Bytes {
				t.Errorf("requestTextBytes(%v) = %d, want %d", c.Body, got, c.Bytes)
			}
		}
	})

	for _, c := range fixtures.Usage {
		t.Run("usage/"+c.Name, func(t *testing.T) {
			// A JSON body as the transports decode it: numbers stay json.Number.
			raw, _ := json.Marshal(c.Payload)
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var payload map[string]any
			_ = decoder.Decode(&payload)
			found, ok := detectUsage(payload)
			if want := c.Expect["found"] == true; ok != want {
				t.Fatalf("found = %v, want %v", ok, want)
			}
			if ok {
				checkFixtureUsage(t, found, c.Expect)
			}
		})
	}

	for _, c := range fixtures.Streams {
		t.Run("stream/"+c.Name, func(t *testing.T) {
			collector := feedFixtureStream(c.SSE)
			if collector.finished != c.Complete || collector.deltaBytes != c.DeliveredByte {
				t.Errorf("complete = %v, delivered = %d, want %v, %d", collector.finished, collector.deltaBytes, c.Complete, c.DeliveredByte)
			}
			found, ok := collector.result(true, 0)
			if want := c.Expect["found"] == true; ok != want {
				t.Fatalf("found = %v, want %v", ok, want)
			}
			if ok {
				checkFixtureUsage(t, found, c.Expect)
			}
		})
	}

	for _, c := range fixtures.Aborted {
		t.Run("aborted/"+c.Name, func(t *testing.T) {
			if got := requestTextBytes(c.RequestBody); got != c.RequestBytes {
				t.Errorf("requestTextBytes = %d, want %d", got, c.RequestBytes)
			}
			collector := feedFixtureStream(c.SSE)
			found, ok := collector.result(false, requestTextBytes(c.RequestBody))
			if want := c.Expect["bill"] == true; ok != want {
				t.Fatalf("billed = %v, want %v", ok, want)
			}
			if !ok {
				return
			}
			checkFixtureUsage(t, found, c.Expect)
			props := found.usage.properties()
			if props["usage_source"] != c.Expect["usage_source"] {
				t.Errorf("usage_source = %v, want %v", props["usage_source"], c.Expect["usage_source"])
			}
			// A stream cut after the provider's final usage already has its full usage: not aborted.
			if aborted := props["stream_aborted"] == true; aborted == collector.finished {
				t.Errorf("stream_aborted = %v with the final marker seen = %v", aborted, collector.finished)
			}
		})
	}
}
