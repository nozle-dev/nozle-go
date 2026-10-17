package nozle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func newJSONRequest(ctx context.Context, method, url string, body interface{}) (*http.Request, error) {
	if body == nil {
		return http.NewRequestWithContext(ctx, method, url, nil)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if b, err = withoutUnset(b); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 4096))
}

// withoutUnset leaves out every field that is null or an empty string, at any depth, so an optional
// field the caller did not set is not sent and the API applies its default. Free-form maps (event
// properties and metadata) are the caller's own data and are sent unchanged.
func withoutUnset(body []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(compact(value))
}

func compact(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if item == nil || item == "" {
				delete(v, key)
				continue
			}
			if key == "properties" || key == "metadata" {
				continue
			}
			v[key] = compact(item)
		}
	case []any:
		for i, item := range v {
			v[i] = compact(item)
		}
	}
	return value
}
